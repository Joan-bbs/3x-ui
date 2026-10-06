package model

import "time"

// TrafficHistory represents inbound traffic statistics record
type TrafficHistory struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	InboundID       uint      `gorm:"index" json:"inbound_id"`
	InboundTag      string    `json:"inbound_tag"`
	UpBytes         int64     `json:"up_bytes"`          // Upstream bytes
	DownBytes       int64     `json:"down_bytes"`        // Downstream bytes
	UpSpeed         float64   `json:"up_speed"`          // Upstream speed (bytes/sec)
	DownSpeed       float64   `json:"down_speed"`        // Downstream speed (bytes/sec)
	ConnectionCount int       `json:"connection_count"`  // Active connections
	CreatedAt       time.Time `gorm:"index" json:"created_at"`
}

// TrafficSnapshot represents current traffic snapshot
type TrafficSnapshot struct {
	InboundID       uint      `json:"inbound_id"`
	InboundTag      string    `json:"inbound_tag"`
	UpBytes         int64     `json:"up_bytes"`
	DownBytes       int64     `json:"down_bytes"`
	UpSpeed         float64   `json:"up_speed"`
	DownSpeed       float64   `json:"down_speed"`
	ConnectionCount int       `json:"connection_count"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TrafficMonitorSettings represents monitoring configuration
type TrafficMonitorSettings struct {
	RefreshInterval int `json:"refresh_interval"` // 1, 5, 10 seconds
	RetentionDays   int `json:"retention_days"`   // 7, 30, 90 days
}
