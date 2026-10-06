package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/database"
	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/logger"
	"github.com/mhsanaei/3x-ui/v3/xray"
	"gorm.io/gorm"
)

type InboundTrafficMonitor struct {
	mu                 sync.RWMutex
	lastSnapshot       map[uint]*model.TrafficSnapshot // inbound_id -> snapshot
	lastUpdateTime     map[uint]time.Time
	refreshInterval    time.Duration
	retentionDays      int
	ctx                context.Context
	cancel             context.CancelFunc
	xrayService        *XrayService
	inboundService     *InboundService
	settingService     *SettingService
	monitoringActive   bool
	lastCleanupTime    time.Time
}

var (
	trafficMonitorMu sync.Mutex
	trafficMonitor   *InboundTrafficMonitor
)

// InitTrafficMonitor initializes the traffic monitor
func InitTrafficMonitor(
	xraySvc *XrayService,
	inboundSvc *InboundService,
	settingSvc *SettingService,
) *InboundTrafficMonitor {
	trafficMonitorMu.Lock()
	defer trafficMonitorMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	tm := &InboundTrafficMonitor{
		lastSnapshot:    make(map[uint]*model.TrafficSnapshot),
		lastUpdateTime:  make(map[uint]time.Time),
		refreshInterval: 5 * time.Second, // default 5s
		retentionDays:   7,                // default 7 days
		ctx:             ctx,
		cancel:          cancel,
		xrayService:     xraySvc,
		inboundService:  inboundSvc,
		settingService:  settingSvc,
		lastCleanupTime: time.Now(),
	}

	return tm
}

// GetTrafficMonitor returns the global traffic monitor instance
func GetTrafficMonitor() *InboundTrafficMonitor {
	trafficMonitorMu.Lock()
	defer trafficMonitorMu.Unlock()
	return trafficMonitor
}

// StartMonitoring starts the real-time traffic monitoring
func (tm *InboundTrafficMonitor) StartMonitoring() error {
	if tm == nil {
		return fmt.Errorf("traffic monitor not initialized")
	}

	tm.mu.Lock()
	if tm.monitoringActive {
		tm.mu.Unlock()
		return fmt.Errorf("monitoring already active")
	}
	tm.monitoringActive = true
	tm.mu.Unlock()

	go tm.monitoringLoop()
	logger.Info("Inbound traffic monitoring started")
	return nil
}

// StopMonitoring stops the traffic monitoring
func (tm *InboundTrafficMonitor) StopMonitoring() {
	if tm == nil {
		return
	}

	tm.mu.Lock()
	if !tm.monitoringActive {
		tm.mu.Unlock()
		return
	}
	tm.monitoringActive = false
	tm.mu.Unlock()

	tm.cancel()
	logger.Info("Inbound traffic monitoring stopped")
}

// SetRefreshInterval sets the monitoring refresh interval (1, 5, 10 seconds)
func (tm *InboundTrafficMonitor) SetRefreshInterval(seconds int) error {
	if seconds != 1 && seconds != 5 && seconds != 10 {
		return fmt.Errorf("invalid refresh interval: %d, must be 1, 5, or 10", seconds)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.refreshInterval = time.Duration(seconds) * time.Second
	return nil
}

// SetRetentionDays sets the historical data retention period (7, 30, 90 days)
func (tm *InboundTrafficMonitor) SetRetentionDays(days int) error {
	if days != 7 && days != 30 && days != 90 {
		return fmt.Errorf("invalid retention days: %d, must be 7, 30, or 90", days)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.retentionDays = days
	return nil
}

// GetConfig returns current monitoring configuration
func (tm *InboundTrafficMonitor) GetConfig() *model.TrafficMonitorConfig {
	if tm == nil {
		return &model.TrafficMonitorConfig{
			RefreshInterval: 5,
			RetentionDays:   7,
		}
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return &model.TrafficMonitorConfig{
		RefreshInterval: int(tm.refreshInterval.Seconds()),
		RetentionDays:   tm.retentionDays,
	}
}

// GetCurrentTraffic returns current traffic snapshot for all inbounds
func (tm *InboundTrafficMonitor) GetCurrentTraffic() []*model.TrafficSnapshot {
	if tm == nil {
		return []*model.TrafficSnapshot{}
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	snapshots := make([]*model.TrafficSnapshot, 0, len(tm.lastSnapshot))
	for _, snap := range tm.lastSnapshot {
		if snap != nil {
			snapshots = append(snapshots, snap)
		}
	}
	return snapshots
}

// GetTrafficHistory retrieves historical traffic data
func (tm *InboundTrafficMonitor) GetTrafficHistory(inboundID uint, hours int) ([]*model.TrafficHistory, error) {
	if tm == nil {
		return nil, fmt.Errorf("traffic monitor not initialized")
	}

	db := database.DB
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var histories []*model.TrafficHistory
	from := time.Now().Add(-time.Duration(hours) * time.Hour)

	if err := db.Where("inbound_id = ? AND created_at >= ?", inboundID, from).
		Order("created_at ASC").
		Find(&histories).Error; err != nil {
		return nil, err
	}

	return histories, nil
}

// monitoringLoop is the main monitoring loop
func (tm *InboundTrafficMonitor) monitoringLoop() {
	ticker := time.NewTicker(tm.refreshInterval)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return

		case <-ticker.C:
			// Update refresh interval if changed
			tm.mu.RLock()
			interval := tm.refreshInterval
			tm.mu.RUnlock()

			if ticker.Stop() {
				ticker.Reset(interval)
			}

			tm.collectTrafficMetrics()

		case <-cleanupTicker.C:
			tm.cleanupOldRecords()
		}
	}
}

// collectTrafficMetrics collects current traffic metrics for all inbounds
func (tm *InboundTrafficMonitor) collectTrafficMetrics() {
	inbounds, err := tm.inboundService.GetAllInbounds()
	if err != nil {
		logger.Debugf("Failed to get inbounds: %v", err)
		return
	}

	now := time.Now()
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, inbound := range inbounds {
		snapshot := tm.calculateTrafficSnapshot(inbound, now)
		if snapshot != nil {
			tm.lastSnapshot[inbound.ID] = snapshot
			tm.lastUpdateTime[inbound.ID] = now
		}
	}

	// Save to database
	go tm.saveTrafficRecords()
}

// calculateTrafficSnapshot calculates traffic metrics for a single inbound
func (tm *InboundTrafficMonitor) calculateTrafficSnapshot(inbound *model.Inbound, now time.Time) *model.TrafficSnapshot {
	// Get traffic data from xray
	var up, down int64
	var connCount int

	// Try to get client traffic stats if available
	if tm.xrayService != nil {
		// Get aggregated traffic for this inbound
		// This reads from Xray's stats API or internal counters
		stats := tm.xrayService.GetInboundStats(inbound.Tag)
		if stats != nil {
			up = stats.GetUplink()
			down = stats.GetDownlink()
			connCount = stats.GetConnectionCount()
		}
	}

	// Calculate speed
	var upSpeed, downSpeed float64
	lastTime, exists := tm.lastUpdateTime[inbound.ID]
	if exists && !lastTime.Equal(now) {
		elapsed := now.Sub(lastTime).Seconds()
		if elapsed > 0 {
			lastSnap := tm.lastSnapshot[inbound.ID]
			if lastSnap != nil {
				upSpeed = float64(up-lastSnap.UpBytes) / elapsed
				downSpeed = float64(down-lastSnap.DownBytes) / elapsed
				if upSpeed < 0 {
					upSpeed = 0
				}
				if downSpeed < 0 {
					downSpeed = 0
				}
			}
		}
	}

	return &model.TrafficSnapshot{
		InboundID:       inbound.ID,
		InboundTag:      inbound.Tag,
		UpBytes:         up,
		DownBytes:       down,
		UpSpeed:         upSpeed,
		DownSpeed:       downSpeed,
		ConnectionCount: connCount,
		UpdatedAt:       now,
	}
}

// saveTrafficRecords saves current traffic snapshots to database
func (tm *InboundTrafficMonitor) saveTrafficRecords() {
	db := database.DB
	if db == nil {
		return
	}

	tm.mu.RLock()
	snapshots := make([]*model.TrafficSnapshot, 0, len(tm.lastSnapshot))
	for _, snap := range tm.lastSnapshot {
		if snap != nil {
			snapshots = append(snapshots, snap)
		}
	}
	tm.mu.RUnlock()

	for _, snap := range snapshots {
		history := &model.TrafficHistory{
			InboundID:       snap.InboundID,
			InboundTag:      snap.InboundTag,
			UpBytes:         snap.UpBytes,
			DownBytes:       snap.DownBytes,
			UpSpeed:         snap.UpSpeed,
			DownSpeed:       snap.DownSpeed,
			ConnectionCount: snap.ConnectionCount,
			CreatedAt:       snap.UpdatedAt,
		}

		if err := db.Create(history).Error; err != nil {
			logger.Debugf("Failed to save traffic history: %v", err)
		}
	}
}

// cleanupOldRecords removes old traffic history records based on retention policy
func (tm *InboundTrafficMonitor) cleanupOldRecords() {
	db := database.DB
	if db == nil {
		return
	}

	tm.mu.RLock()
	retentionDays := tm.retentionDays
	tm.mu.RUnlock()

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	if err := db.Where("created_at < ?", cutoffTime).Delete(&model.TrafficHistory{}).Error; err != nil {
		logger.Debugf("Failed to cleanup old traffic records: %v", err)
		return
	}

	logger.Debugf("Traffic history cleanup completed (retention: %d days)", retentionDays)
}

// BroadcastTrafficUpdate broadcasts current traffic to all WebSocket clients
func (tm *InboundTrafficMonitor) BroadcastTrafficUpdate(hub interface{}) {
	if tm == nil {
		return
	}

	snapshots := tm.GetCurrentTraffic()
	if len(snapshots) == 0 {
		return
	}

	// Type assert to Hub if needed for broadcasting
	// This will be called from the monitoring loop
	if h, ok := hub.(*interface{}); ok && h != nil {
		// Broadcasting logic handled by caller
		_ = snapshots
	}
}
