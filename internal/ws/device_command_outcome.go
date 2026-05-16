package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const agentMsgDeviceCommandResult = "device_command_result"

type agentDeviceCommandResult struct {
	Type      string `json:"type"`
	CommandID string `json:"command_id"`
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
	Error     string `json:"error"`
}

// ApplyDeviceCommandOutcome updates device_commands when an agent reports execution results.
func ApplyDeviceCommandOutcome(ctx context.Context, pool *pgxpool.Pool, dash *DashboardHub, certSerial string, raw []byte, logger *slog.Logger) bool {
	if pool == nil {
		return false
	}
	var probe agentDeviceCommandResult
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.Type) != agentMsgDeviceCommandResult {
		return false
	}
	cmdIDStr := strings.TrimSpace(probe.CommandID)
	if cmdIDStr == "" {
		return true
	}
	cmdID, err := uuid.Parse(cmdIDStr)
	if err != nil {
		return true
	}
	certSerial = strings.TrimSpace(certSerial)
	owned, ownErr := database.DeviceCommandOwnedByCertSerial(ctx, pool, cmdID, certSerial)
	if ownErr != nil {
		if logger != nil {
			logger.Error("device command ownership check failed", "err", ownErr, "command_id", cmdIDStr)
		}
		return true
	}
	if !owned {
		if logger != nil {
			logger.Warn("device command result rejected: cert_serial mismatch", "command_id", cmdIDStr, "cert_serial", certSerial)
		}
		return true
	}

	output := strings.TrimSpace(probe.Output)
	if output == "" && !probe.OK {
		output = strings.TrimSpace(probe.Error)
	}
	if output == "" && probe.OK {
		output = "ok"
	}

	var updated models.DeviceCommand
	if probe.OK {
		updated, err = database.CompleteDeviceCommand(ctx, pool, cmdID, output)
	} else {
		if output == "" {
			output = "command failed"
		}
		updated, err = database.FailDeviceCommand(ctx, pool, cmdID, output)
	}
	if err != nil {
		if logger != nil {
			logger.Error("device command outcome update failed", "err", err, "command_id", cmdIDStr)
		}
		return true
	}

	if dash != nil {
		hid, hidErr := humanIDByCertSerial(ctx, pool, certSerial)
		if hidErr == nil && strings.TrimSpace(hid) != "" {
			b, encErr := MarshalDeviceCommandUpdate(hid, updated)
			if encErr == nil {
				dash.Broadcast(b)
			}
		}
	}
	return true
}
