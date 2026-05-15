package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const agentMsgTelemetry = "telemetry"

// AgentTelemetryDeps configures WebSocket-originated telemetry handling on the server.
type AgentTelemetryDeps struct {
	Pool            *pgxpool.Pool
	Logger          *slog.Logger
	AdvisoryLockKey int64
	OnHeartbeat     func(ctx context.Context, assetID uuid.UUID)
	OnAccepted      func(certSerial, humanID string, assetID uuid.UUID, payload api.TelemetryPayload)
}

// tryHandleAgentTelemetry persists agent telemetry JSON received on the C2 WebSocket.
func tryHandleAgentTelemetry(ctx context.Context, certSerial string, data []byte, d AgentTelemetryDeps) bool {
	if d.Pool == nil {
		return false
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.Type) != agentMsgTelemetry {
		return false
	}

	var wire struct {
		Type               string  `json:"type"`
		UptimeSeconds      uint64  `json:"uptime_seconds,omitempty"`
		RootDiskTotalBytes uint64  `json:"root_disk_total_bytes,omitempty"`
		RootDiskFreeBytes  uint64  `json:"root_disk_free_bytes,omitempty"`
		RootDiskUsedBytes  uint64  `json:"root_disk_used_bytes,omitempty"`
		api.TelemetryPayload
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		if d.Logger != nil {
			d.Logger.Warn("agent telemetry decode failed", "cert_serial", certSerial, "err", err)
		}
		return true
	}

	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := api.ProcessTelemetry(opCtx, api.TelemetryProcessDeps{
		Pool:            d.Pool,
		AdvisoryLockKey: d.AdvisoryLockKey,
		OnHeartbeat:     d.OnHeartbeat,
		OnAccepted:      d.OnAccepted,
	}, certSerial, wire.TelemetryPayload)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("agent websocket telemetry failed",
				"err", err,
				"cert_serial", certSerial,
				"hostname", wire.Hostname,
			)
		}
		return true
	}

	if d.Logger != nil {
		d.Logger.Info("agent websocket telemetry accepted",
			"asset_id", result.AssetID.String(),
			"human_id", result.HumanID,
			"cert_serial", certSerial,
		)
	}
	return true
}
