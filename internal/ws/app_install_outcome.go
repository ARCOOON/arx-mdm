package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const agentMsgInstallAppResult = "install_app_result"

type agentInstallAppResult struct {
	Type      string `json:"type"`
	AppID     string `json:"app_id"`
	OK        bool   `json:"ok"`
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error,omitempty"`
}

// ApplyInstallAppOutcome persists install_app uplink notifications into device_apps.
func ApplyInstallAppOutcome(ctx context.Context, pool *pgxpool.Pool, certSerial string, raw []byte, logger *slog.Logger) bool {
	if pool == nil {
		return false
	}
	var probe agentInstallAppResult
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.Type) != agentMsgInstallAppResult {
		return false
	}
	appIDRaw := strings.TrimSpace(probe.AppID)
	appID, err := uuid.Parse(appIDRaw)
	if err != nil {
		return true
	}
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return true
	}

	status := "success"
	var errMsg *string
	if !probe.OK {
		status = "failed"
		em := strings.TrimSpace(probe.Error)
		if em == "" {
			em = "install failed"
		}
		errMsg = &em
	}

	_, execErr := pool.Exec(ctx, `
UPDATE device_apps da
SET status = $3,
    error_message = $4,
    last_updated = now()
FROM assets a
WHERE da.app_id = $1
  AND da.device_id = a.id
  AND a.cert_serial = $2
`, appID, certSerial, status, errMsg)
	if execErr != nil {
		if logger != nil {
			logger.Error("device_apps install outcome update failed", "err", execErr, "app_id", appIDRaw)
		}
		return true
	}
	return true
}
