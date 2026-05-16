package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const agentMsgPackageResult = "package_result"

type agentPackageResult struct {
	Type         string `json:"type"`
	DeploymentID string `json:"deployment_id"`
	RequestID    string `json:"request_id"`
	OK           bool   `json:"ok"`
	Error        string `json:"error"`
	Operation    string `json:"operation"`
	PackageType  string `json:"package_type"`
}

// ApplyPackageDeploymentOutcome updates deployments when an agent reports completion.
func ApplyPackageDeploymentOutcome(ctx context.Context, pool *pgxpool.Pool, certSerial string, raw []byte, logger *slog.Logger) bool {
	if pool == nil {
		return false
	}
	var probe agentPackageResult
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.Type) != agentMsgPackageResult {
		return false
	}
	depID := strings.TrimSpace(probe.DeploymentID)
	if depID == "" {
		return true
	}
	id, parseErr := uuid.Parse(depID)
	if parseErr != nil {
		return true
	}
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return true
	}

	status := "succeeded"
	var errMsg *string
	if !probe.OK {
		status = "failed"
		em := strings.TrimSpace(probe.Error)
		if em != "" {
			errMsg = &em
		} else {
			em = "operation failed"
			errMsg = &em
		}
	}

	tag, err := pool.Exec(ctx, `
UPDATE deployments d
SET status = $3,
    error_message = $4,
    updated_at = now()
FROM assets a
WHERE d.id = $1
  AND d.asset_id = a.id
  AND a.cert_serial = $2
`, id, certSerial, status, errMsg)
	if err != nil {
		if logger != nil {
			logger.Error("deployment outcome update failed", "err", err, "deployment_id", depID)
		}
		return true
	}
	if tag.RowsAffected() == 0 && logger != nil {
		logger.Debug("deployment outcome ignored (no matching row)", "deployment_id", depID, "cert_serial", certSerial)
	}
	return true
}
