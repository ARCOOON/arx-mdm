package c2

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

const uplinkTypeResult = "device_command_result"

// JSONWriter sends uplink JSON to the C2 server.
type JSONWriter func(v any) error

type deviceCommandDownlink struct {
	Action      string `json:"action"`
	CommandID   string `json:"command_id"`
	CommandType string `json:"command_type"`
	Payload     string `json:"payload"`
}

// HandleDownlink executes a device_command message from the server.
func HandleDownlink(ctx context.Context, logger *slog.Logger, write JSONWriter, raw []byte) error {
	var cmd deviceCommandDownlink
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return fmt.Errorf("c2: decode device_command: %w", err)
	}
	commandID := strings.TrimSpace(cmd.CommandID)
	commandType := strings.TrimSpace(strings.ToLower(cmd.CommandType))
	if commandID == "" {
		return fmt.Errorf("c2: command_id is required")
	}
	if commandType == "" {
		return reportResult(write, commandID, false, "", "command_type is required")
	}

	run := func() {
		out, err := dispatch(ctx, commandType, cmd.Payload)
		if err != nil {
			if logger != nil {
				logger.Error("device command failed", "command_id", commandID, "type", commandType, "err", err)
			}
			_ = reportResult(write, commandID, false, out, err.Error())
			return
		}
		if err := reportResult(write, commandID, true, out, ""); err != nil && logger != nil {
			logger.Debug("device command result uplink failed", "err", err)
		}
	}

	switch commandType {
	case "ping":
		run()
	case "reboot":
		if err := reportResult(write, commandID, true, "reboot scheduled", ""); err != nil && logger != nil {
			logger.Debug("device command reboot ack failed", "err", err)
		}
		go func() {
			if _, err := executeReboot(); err != nil && logger != nil {
				logger.Error("device command reboot failed", "command_id", commandID, "err", err)
			}
		}()
	case "script":
		go run()
	case "restart_service":
		body, berr := BuildRestartServiceScript(cmd.Payload)
		if berr != nil {
			_ = reportResult(write, commandID, false, "", berr.Error())
			break
		}
		go func() {
			out, err := executeScript(context.Background(), body)
			if err != nil {
				if logger != nil {
					logger.Error("device command restart_service failed", "command_id", commandID, "err", err)
				}
				_ = reportResult(write, commandID, false, out, err.Error())
				return
			}
			_ = reportResult(write, commandID, true, out, "")
		}()
	case "push_config":
		body, berr := BuildPushConfigScript(cmd.Payload)
		if berr != nil {
			_ = reportResult(write, commandID, false, "", berr.Error())
			break
		}
		go func() {
			out, err := executeScript(context.Background(), body)
			if err != nil {
				if logger != nil {
					logger.Error("device command push_config failed", "command_id", commandID, "err", err)
				}
				_ = reportResult(write, commandID, false, out, err.Error())
				return
			}
			_ = reportResult(write, commandID, true, out, "")
		}()
	default:
		_ = reportResult(write, commandID, false, "", fmt.Sprintf("unsupported command_type %q", commandType))
	}
	return nil
}

func dispatch(ctx context.Context, commandType, payload string) (string, error) {
	switch commandType {
	case "ping":
		return executePing()
	case "reboot":
		return executeReboot()
	case "script":
		return executeScript(ctx, payload)
	case "restart_service":
		body, err := BuildRestartServiceScript(payload)
		if err != nil {
			return "", err
		}
		return executeScript(ctx, body)
	case "push_config":
		body, err := BuildPushConfigScript(payload)
		if err != nil {
			return "", err
		}
		return executeScript(ctx, body)
	default:
		return "", fmt.Errorf("unsupported command_type %q", commandType)
	}
}

func executePing() (string, error) {
	return "pong", nil
}

func reportResult(write JSONWriter, commandID string, ok bool, output, errMsg string) error {
	if write == nil {
		return fmt.Errorf("c2: no uplink writer")
	}
	if !ok && output == "" {
		output = errMsg
	}
	return write(map[string]any{
		"type":       uplinkTypeResult,
		"command_id": commandID,
		"ok":         ok,
		"output":     output,
		"error":      errMsg,
	})
}
