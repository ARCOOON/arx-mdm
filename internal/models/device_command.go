package models

import (
	"time"

	"github.com/google/uuid"
)

// Device command types and lifecycle statuses (device_commands table).
const (
	DeviceCommandTypePing   = "ping"
	DeviceCommandTypeReboot = "reboot"
	DeviceCommandTypeScript = "script"

	DeviceCommandStatusPending   = "pending"
	DeviceCommandStatusSent      = "sent"
	DeviceCommandStatusCompleted = "completed"
	DeviceCommandStatusFailed    = "failed"
)

// DeviceCommand is a queued remote instruction for a managed asset.
type DeviceCommand struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	DeviceID    uuid.UUID  `json:"device_id" db:"device_id"`
	CommandType string     `json:"command_type" db:"command_type"`
	Payload     string     `json:"payload,omitempty" db:"payload"`
	Status      string     `json:"status" db:"status"`
	Output      string     `json:"output,omitempty" db:"output"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}
