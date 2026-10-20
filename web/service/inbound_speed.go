package service

import (
	"encoding/json"
	"time"

	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/web/monitor"
	"github.com/mhsanaei/3x-ui/v2/xray"
)

type inboundSpeedState struct {
	tracker  monitor.Tracker
	snapshot InboundSpeedSnapshot
	at       time.Time
}

// All access is protected by the Xray process lock, including quota resets.
var inboundSpeedCache inboundSpeedState

// GetInboundsForMonitor avoids loading client statistics and configuration on
// every refresh. Ownership is enforced in the database query.
func (s *InboundService) GetInboundsForMonitor(userID int) ([]*model.Inbound, error) {
	var inbounds []*model.Inbound
	err := database.GetDB().Model(&model.Inbound{}).
		Select("id", "remark", "port", "listen", "protocol", "enable", "tag").
		Where("user_id = ?", userID).Order("port ASC, id ASC").Find(&inbounds).Error
	return inbounds, err
}

// InboundSpeedSnapshot is shared between authenticated monitor requests.
// Rates are keyed by tag internally; controllers filter them by inbound ownership.
type InboundSpeedSnapshot struct {
	Rates        map[string]monitor.Rate
	Ready        bool
	Running      bool
	StatsEnabled bool
	SampledAt    int64
}

func inboundCounters(traffic []*xray.Traffic) map[string]monitor.Counter {
	counters := make(map[string]monitor.Counter)
	for _, item := range traffic {
		if item != nil && item.IsInbound {
			counters[item.Tag] = monitor.Counter{Up: item.Up, Down: item.Down}
		}
	}
	return counters
}

// GetInboundSpeeds samples without clearing counters or changing quota accounting.
// Requests within two seconds share one sample, so multiple tabs do not shorten
// the sampling interval or multiply gRPC traffic.
func (s *XrayService) GetInboundSpeeds() (InboundSpeedSnapshot, error) {
	lock.Lock()
	defer lock.Unlock()
	if !s.IsXrayRunning() {
		inboundSpeedCache = inboundSpeedState{}
		return InboundSpeedSnapshot{}, nil
	}
	var policy struct {
		System struct {
			Up   bool `json:"statsInboundUplink"`
			Down bool `json:"statsInboundDownlink"`
		} `json:"system"`
	}
	if err := json.Unmarshal(p.GetConfig().Policy, &policy); err != nil || !policy.System.Up || !policy.System.Down {
		inboundSpeedCache = inboundSpeedState{}
		return InboundSpeedSnapshot{Running: true}, nil
	}
	if !inboundSpeedCache.at.IsZero() && time.Since(inboundSpeedCache.at) < 2*time.Second {
		return inboundSpeedCache.snapshot, nil
	}
	// Use a request-local client rather than sharing a mutable API connection.
	var api xray.XrayAPI
	if err := api.Init(p.GetAPIPort()); err != nil {
		return InboundSpeedSnapshot{}, err
	}
	defer api.Close()
	traffic, err := api.GetInboundTraffic()
	if err != nil {
		inboundSpeedCache = inboundSpeedState{}
		return InboundSpeedSnapshot{}, err
	}
	now := time.Now()
	rates, ready := inboundSpeedCache.tracker.Sample(now, inboundCounters(traffic))
	snapshot := InboundSpeedSnapshot{
		Rates: rates, Ready: ready, Running: true, StatsEnabled: true, SampledAt: now.UnixMilli(),
	}
	inboundSpeedCache.at = now
	inboundSpeedCache.snapshot = snapshot
	return snapshot, nil
}
