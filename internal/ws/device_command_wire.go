package ws

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/models"
)

const MsgTypeDeviceCommandUpdate = "device_command_update"

// DeviceCommandWire is a dashboard-facing command row.
type DeviceCommandWire struct {
	ID          string  `json:"id"`
	DeviceID    string  `json:"device_id"`
	CommandType string  `json:"command_type"`
	Payload     string  `json:"payload,omitempty"`
	Status      string  `json:"status"`
	Output      string  `json:"output,omitempty"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at,omitempty"`
	TargetArxID string  `json:"target_arx_id"`
}

// DeviceCommandUpdateMsg notifies dashboards that a command finished or changed.
type DeviceCommandUpdateMsg struct {
	Type    string            `json:"type"`
	Command DeviceCommandWire `json:"command"`
}

// MarshalDeviceCommandUpdate encodes a live command update for dashboard subscribers.
func MarshalDeviceCommandUpdate(targetArxID string, cmd models.DeviceCommand) ([]byte, error) {
	wire := deviceCommandToWire(cmd, targetArxID)
	return json.Marshal(DeviceCommandUpdateMsg{
		Type:    MsgTypeDeviceCommandUpdate,
		Command: wire,
	})
}

func deviceCommandToWire(cmd models.DeviceCommand, targetArxID string) DeviceCommandWire {
	wire := DeviceCommandWire{
		ID:          cmd.ID.String(),
		DeviceID:    cmd.DeviceID.String(),
		CommandType: cmd.CommandType,
		Payload:     cmd.Payload,
		Status:      cmd.Status,
		Output:      cmd.Output,
		CreatedAt:   cmd.CreatedAt.UTC().Format(time.RFC3339),
		TargetArxID: strings.TrimSpace(targetArxID),
	}
	if cmd.CompletedAt != nil {
		s := cmd.CompletedAt.UTC().Format(time.RFC3339)
		wire.CompletedAt = &s
	}
	return wire
}
