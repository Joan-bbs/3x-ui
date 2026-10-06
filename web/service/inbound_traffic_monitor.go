package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/database"
	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/logger"
)

type InboundTrafficMonitor struct {
	mu              sync.RWMutex
	lastSnapshot    map[int]*model.TrafficHistory
	lastUpdateTime  map[int]time.Time
	refreshInterval time.Duration
	retentionDays   int

	inboundService *InboundService
	ctx            context.Context
	cancel         context.CancelFunc

	started bool
}

var (
	monitorMu sync.Mutex
	monitor   *InboundTrafficMonitor
)

func InitInboundTrafficMonitor(inboundService *InboundService) *InboundTrafficMonitor {
	monitorMu.Lock()
	defer monitorMu.Unlock()

	if monitor != nil {
		return monitor
	}

	ctx, cancel := context.WithCancel(context.Background())
	monitor = &InboundTrafficMonitor{
		lastSnapshot:    make(map[int]*model.TrafficHistory),
		lastUpdateTime:  make(map[int]time.Time),
		refreshInterval: 5 * time.Second,
		retentionDays:   7,
		inboundService:  inboundService,
		ctx:             ctx,
		cancel:          cancel,
	}
	return monitor
}

func GetInboundTrafficMonitor() *InboundTrafficMonitor {
	return monitor
}

func (m *InboundTrafficMonitor) SetRefreshInterval(seconds int) error {
	if seconds != 1 && seconds != 5 && seconds != 10 {
		return fmt.Errorf("refresh interval must be 1, 5 or 10 seconds")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshInterval = time.Duration(seconds) * time.Second
	return nil
}

func (m *InboundTrafficMonitor) SetRetentionDays(days int) error {
	if days != 7 && days != 30 && days != 90 {
		return fmt.Errorf("retention must be 7, 30 or 90 days")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retentionDays = days
	return nil
}

func (m *InboundTrafficMonitor) GetConfig() model.TrafficMonitorSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return model.TrafficMonitorSettings{
		RefreshInterval: int(m.refreshInterval.Seconds()),
		RetentionDays:   m.retentionDays,
	}
}

func (m *InboundTrafficMonitor) Start() error {
	if m == nil {
		return fmt.Errorf("monitor is nil")
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.mu.Unlock()

	go m.loop()
	logger.Info("Inbound traffic monitor started")
	return nil
}

func (m *InboundTrafficMonitor) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false
	m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}
	logger.Info("Inbound traffic monitor stopped")
}

func (m *InboundTrafficMonitor) loop() {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.collect()
		case <-cleanupTicker.C:
			m.cleanupExpiredHistory()
		}
	}
}

func (m *InboundTrafficMonitor) collect() {
	if m == nil || m.inboundService == nil {
		return
	}

	inbounds, err := m.inboundService.GetAllInbounds()
	if err != nil {
		logger.Debugf("get all inbounds failed: %v", err)
		return
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, inbound := range inbounds {
		if inbound == nil {
			continue
		}

		snap := &model.TrafficHistory{
			InboundID:       uint(inbound.Id),
			InboundTag:      inbound.Tag,
			UpBytes:         inbound.Up,
			DownBytes:       inbound.Down,
			ConnectionCount: m.estimateActiveConnections(inbound),
			CreatedAt:       now.UnixMilli(),
		}

		if prev, ok := m.lastSnapshot[inbound.Id]; ok {
			if prevTime, ok2 := m.lastUpdateTime[inbound.Id]; ok2 {
				elapsed := now.Sub(prevTime).Seconds()
				if elapsed > 0 {
					snap.UpSpeed = float64(snap.UpBytes-prev.UpBytes) / elapsed
					snap.DownSpeed = float64(snap.DownBytes-prev.DownBytes) / elapsed

					if snap.UpSpeed < 0 {
						snap.UpSpeed = 0
					}
					if snap.DownSpeed < 0 {
						snap.DownSpeed = 0
					}
				}
			}
		}

		m.lastSnapshot[inbound.Id] = snap
		m.lastUpdateTime[inbound.Id] = now
	}

	m.persistCurrent()
}

func (m *InboundTrafficMonitor) estimateActiveConnections(inbound *model.Inbound) int {
	if inbound == nil {
		return 0
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return 0
	}

	clients, ok := settings["clients"].([]any)
	if !ok {
		return 0
	}

	count := 0
	for _, item := range clients {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if enable, ok2 := c["enable"].(bool); ok2 && enable {
			count++
		}
	}
	return count
}

func (m *InboundTrafficMonitor) persistCurrent() {
	db := database.GetDB()
	if db == nil {
		return
	}

	for _, snap := range m.lastSnapshot {
		if snap == nil {
			continue
		}
		if err := db.Create(snap).Error; err != nil {
			logger.Debugf("save traffic monitor history failed: %v", err)
		}
	}
}

func (m *InboundTrafficMonitor) cleanupExpiredHistory() {
	db := database.GetDB()
	if db == nil {
		return
	}

	m.mu.RLock()
	retentionDays := m.retentionDays
	m.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -retentionDays).UnixMilli()
	if err := db.Where("created_at < ?", cutoff).
		Delete(&model.TrafficHistory{}).Error; err != nil {
		logger.Debugf("cleanup traffic history failed: %v", err)
	}
}

func (m *InboundTrafficMonitor) GetCurrentTraffic() []*model.TrafficHistory {
	if m == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*model.TrafficHistory, 0, len(m.lastSnapshot))
	for _, v := range m.lastSnapshot {
		if v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (m *InboundTrafficMonitor) GetHistoryByInbound(inboundID int, hours int) ([]*model.TrafficHistory, error) {
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	var rows []*model.TrafficHistory
	if err := db.Where("inbound_id = ? AND created_at >= ?", inboundID, since).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
