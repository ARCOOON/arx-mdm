package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantAutoQuarantine returns whether automatic quarantine is enabled for the single-tenant row.
func TenantAutoQuarantine(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	if pool == nil {
		return false, fmt.Errorf("database: pool is required")
	}
	var on bool
	err := pool.QueryRow(ctx, `
SELECT COALESCE(auto_quarantine_on_noncompliance, false) FROM tenant_settings WHERE singleton = 1 LIMIT 1
`).Scan(&on)
	if err != nil {
		return false, fmt.Errorf("database: tenant settings: %w", err)
	}
	return on, nil
}

// SetTenantAutoQuarantine updates the global auto-quarantine toggle.
func SetTenantAutoQuarantine(ctx context.Context, pool *pgxpool.Pool, enabled bool) error {
	if pool == nil {
		return fmt.Errorf("database: pool is required")
	}
	_, err := pool.Exec(ctx, `
INSERT INTO tenant_settings (singleton, auto_quarantine_on_noncompliance, updated_at)
VALUES (1, $1, now())
ON CONFLICT (singleton) DO UPDATE SET
  auto_quarantine_on_noncompliance = EXCLUDED.auto_quarantine_on_noncompliance,
  updated_at = now()
`, enabled)
	if err != nil {
		return fmt.Errorf("database: update tenant settings: %w", err)
	}
	return nil
}
