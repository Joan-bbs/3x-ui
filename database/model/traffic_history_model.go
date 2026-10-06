package model

type TrafficHistory struct {
	ID              uint    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundID       uint    `json:"inboundId" gorm:"index;column:inbound_id"`
	InboundTag      string  `json:"inboundTag" gorm:"column:inbound_tag"`
	UpBytes         int64   `json:"upBytes" gorm:"column:up_bytes;default:0"`
	DownBytes       int64   `json:"downBytes" gorm:"column:down_bytes;default:0"`
	UpSpeed         float64 `json:"upSpeed" gorm:"column:up_speed;default:0"`
	DownSpeed       float64 `json:"downSpeed" gorm:"column:down_speed;default:0"`
	ConnectionCount int     `json:"connectionCount" gorm:"column:connection_count;default:0"`
	CreatedAt       int64   `json:"createdAt" gorm:"autoCreateTime:milli;index;column:created_at"`
}

type TrafficMonitorSettings struct {
	RefreshInterval int `json:"refreshInterval"`
	RetentionDays   int `json:"retentionDays"`
}
