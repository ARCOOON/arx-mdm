package models

import (
	"time"

	"github.com/google/uuid"
)

// DeviceMetric is one persisted telemetry sample for a managed asset (device).
type DeviceMetric struct {
	ID        uuid.UUID `json:"id" db:"id"`
	DeviceID  uuid.UUID `json:"device_id" db:"device_id"`
	CPUUsage  float64   `json:"cpu_usage" db:"cpu_usage"`
	RAMTotal  int64     `json:"ram_total" db:"ram_total"`
	RAMUsed   int64     `json:"ram_used" db:"ram_used"`
	DiskTotal int64     `json:"disk_total" db:"disk_total"`
	DiskUsed  int64     `json:"disk_used" db:"disk_used"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
