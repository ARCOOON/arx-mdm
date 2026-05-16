package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileProfileAssignmentStates resets scoped assignments to ok and marks conflicts for participating profiles.
func ReconcileProfileAssignmentStates(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, platform string, conflictingProfiles []uuid.UUID) error {
	if pool == nil {
		return fmt.Errorf("pool is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reconcile assignment states: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	resetSQL := `
WITH affected AS (
    SELECT pa.id
    FROM profile_assignments pa
    JOIN configuration_profiles cp ON cp.id = pa.profile_id
    LEFT JOIN principal_group_devices pgd
           ON pa.target_kind = 'principal_group'
          AND pa.principal_group_id = pgd.group_id
          AND pgd.device_id = $1::uuid
    WHERE lower(trim(cp.platform)) = lower(trim($2))
      AND (
            (pa.target_kind = 'device' AND pa.device_id = $1::uuid)
         OR (pa.target_kind = 'principal_group' AND pgd.device_id IS NOT NULL)
      )
)
UPDATE profile_assignments p
SET assignment_state = 'ok'
FROM affected a
WHERE p.id = a.id`
	if _, err := tx.Exec(ctx, resetSQL, assetID, platform); err != nil {
		return fmt.Errorf("reset assignment states: %w", err)
	}

	if len(conflictingProfiles) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit reconcile assignment states: %w", err)
		}
		return nil
	}

	conflictSQL := `
WITH affected AS (
    SELECT pa.id
    FROM profile_assignments pa
    JOIN configuration_profiles cp ON cp.id = pa.profile_id
    LEFT JOIN principal_group_devices pgd
           ON pa.target_kind = 'principal_group'
          AND pa.principal_group_id = pgd.group_id
          AND pgd.device_id = $1::uuid
    WHERE lower(trim(cp.platform)) = lower(trim($2))
      AND pa.profile_id = ANY($3::uuid[])
      AND (
            (pa.target_kind = 'device' AND pa.device_id = $1::uuid)
         OR (pa.target_kind = 'principal_group' AND pgd.device_id IS NOT NULL)
      )
)
UPDATE profile_assignments p
SET assignment_state = 'conflict'
FROM affected a
WHERE p.id = a.id`
	if _, err := tx.Exec(ctx, conflictSQL, assetID, platform, conflictingProfiles); err != nil {
		return fmt.Errorf("flag conflicting assignments: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reconcile assignment states: %w", err)
	}
	return nil
}
