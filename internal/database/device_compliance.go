package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ComplianceStatusCompliant    = "compliant"
	ComplianceStatusNonCompliant = "non_compliant"
	ComplianceStatusEvaluating   = "evaluating"
)

// SetAssetCompliance updates compliance columns on assets.
func SetAssetCompliance(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, status, reason string) error {
	if pool == nil {
		return fmt.Errorf("database: pool is required")
	}
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case ComplianceStatusCompliant, ComplianceStatusNonCompliant, ComplianceStatusEvaluating:
	default:
		return fmt.Errorf("database: invalid compliance_status %q", status)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 8192 {
		reason = reason[:8192]
	}
	_, err := pool.Exec(ctx, `
UPDATE assets
SET compliance_status = $2,
    compliance_reason = $3,
    updated_at = now()
WHERE id = $1
`, assetID, status, reason)
	if err != nil {
		return fmt.Errorf("database: set compliance: %w", err)
	}
	return nil
}

// SetAssetQuarantineEnabled persists manual or automation quarantine toggle.
func SetAssetQuarantineEnabled(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, enabled bool) error {
	if pool == nil {
		return fmt.Errorf("database: pool is required")
	}
	_, err := pool.Exec(ctx, `
UPDATE assets SET quarantine_enabled = $2, updated_at = now() WHERE id = $1
`, assetID, enabled)
	if err != nil {
		return fmt.Errorf("database: set quarantine_enabled: %w", err)
	}
	return nil
}

// AssetComplianceRow is a snapshot of compliance fields for dashboard fan-out.
type AssetComplianceRow struct {
	Status            string
	Reason            string
	QuarantineEnabled bool
}

// GetAssetCompliance returns compliance columns for one asset.
func GetAssetCompliance(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) (AssetComplianceRow, error) {
	var r AssetComplianceRow
	if pool == nil {
		return r, fmt.Errorf("database: pool is required")
	}
	err := pool.QueryRow(ctx, `
SELECT compliance_status, COALESCE(compliance_reason, ''), quarantine_enabled
FROM assets WHERE id = $1 LIMIT 1
`, assetID).Scan(&r.Status, &r.Reason, &r.QuarantineEnabled)
	if err != nil {
		return r, fmt.Errorf("database: get compliance: %w", err)
	}
	return r, nil
}
