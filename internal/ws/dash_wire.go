package ws

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"

	"github.com/google/uuid"
)

// Dashboard wire message types.
const (
	MsgTypeAssetSnapshot    = "asset_snapshot"
	MsgTypeIncidentSnapshot = "incident_snapshot"
	MsgTypeTelemetryUpdate  = "telemetry_update"
	MsgTypeCommandResult    = "command_result"
	MsgTypePtyStarted       = "pty_started"
)

// AssetWire is a compact asset row for the operator dashboard.
type AssetWire struct {
	ID                string                      `json:"id,omitempty"`
	HumanID           string                      `json:"human_id"`
	Hostname          string                      `json:"hostname"`
	OsType            string                      `json:"os_type,omitempty"`
	OS                string                      `json:"os"`
	CPUModel          string                      `json:"cpu_model"`
	CPULogicalCores   int                         `json:"cpu_logical_cores"`
	CPUUsagePercent   float64                     `json:"cpu_usage_percent"`
	TotalRAMBytes     uint64                      `json:"total_ram_bytes"`
	MemoryUsedBytes   uint64                      `json:"memory_used_bytes"`
	LastSeenRFC3339   string                      `json:"last_seen,omitempty"`
	C2Connected       bool                        `json:"c2_connected"`
	InstalledSoftware []api.TelemetryInstalledApp `json:"installed_software"`
}

// AssetSnapshotMsg is the initial payload after dashboard connect.
type AssetSnapshotMsg struct {
	Type   string      `json:"type"`
	Assets []AssetWire `json:"assets"`
}

// IncidentWire is a compact incident row for the operator dashboard.
type IncidentWire struct {
	ID               string  `json:"id"`
	IncidentNumber   string  `json:"incident_number"`
	ShortDescription string  `json:"short_description"`
	State            string  `json:"state"`
	Priority         int     `json:"priority"`
	SLADue           string  `json:"sla_due"`
	LinkedArxID      *string `json:"linked_arx_id,omitempty"`
	CreatedAt        string  `json:"created_at"`
}

// IncidentSnapshotMsg is sent once after the asset snapshot (incident feed for dashboard).
type IncidentSnapshotMsg struct {
	Type      string         `json:"type"`
	Incidents []IncidentWire `json:"incidents"`
}

// TelemetryUpdateMsg is pushed after each successful POST /v1/telemetry.
type TelemetryUpdateMsg struct {
	Type  string    `json:"type"`
	Asset AssetWire `json:"asset"`
}

// CommandResultMsg acknowledges a dashboard command.
type CommandResultMsg struct {
	Type    string `json:"type"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// MarshalTelemetryUpdate builds a telemetry_update JSON message for dashboards.
func MarshalTelemetryUpdate(c2 *Hub, certSerial, humanID string, assetID uuid.UUID, p api.TelemetryPayload) ([]byte, error) {
	c2On := false
	if c2 != nil {
		for _, s := range c2.ConnectedCertSerials() {
			if s == certSerial {
				c2On = true
				break
			}
		}
	}
	osType := api.DeriveAssetOsType(p)
	a := AssetWire{
		HumanID:           humanID,
		Hostname:          p.Hostname,
		OsType:            osType,
		OS:                strings.TrimSpace(p.OSFamily + " " + p.OSVersion),
		CPUModel:          p.CPUModel,
		CPULogicalCores:   p.CPULogicalCores,
		CPUUsagePercent:   p.CPUUsagePercent,
		TotalRAMBytes:     p.TotalRAMBytes,
		MemoryUsedBytes:   p.MemoryUsedBytes,
		LastSeenRFC3339:   time.Now().UTC().Format(time.RFC3339),
		C2Connected:       c2On,
		InstalledSoftware: p.InstalledSoftware,
	}
	if assetID != uuid.Nil {
		a.ID = assetID.String()
	}
	return json.Marshal(TelemetryUpdateMsg{Type: MsgTypeTelemetryUpdate, Asset: a})
}
