package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FlushPendingDeviceCommands pushes pending device_command rows after an agent reconnects.
func FlushPendingDeviceCommands(ctx context.Context, pool *pgxpool.Pool, hub *Hub, certSerial string, logger *slog.Logger) {
	if pool == nil || hub == nil {
		return
	}
	pending, err := database.ListPendingDeviceCommandsForCertSerial(ctx, pool, certSerial, 25)
	if err != nil {
		if logger != nil {
			logger.Warn("flush pending device commands: list failed", "cert_serial", certSerial, "err", err)
		}
		return
	}
	for _, row := range pending {
		cmd := row.Command
		downlink := map[string]string{
			"action":       "device_command",
			"command_id":   cmd.ID.String(),
			"command_type": cmd.CommandType,
			"payload":      cmd.Payload,
		}
		if !hub.DispatchJSON(certSerial, downlink) {
			if logger != nil {
				logger.Debug("flush pending device commands: dispatch skipped", "command_id", cmd.ID.String())
			}
			continue
		}
		sentCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := database.MarkDeviceCommandSent(sentCtx, pool, cmd.ID); err != nil && logger != nil {
			logger.Warn("flush pending device commands: mark sent failed", "command_id", cmd.ID.String(), "err", err)
		}
		cancel()
	}
}
