package database

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLogFilter selects audit rows for admin listing APIs.
type AuditLogFilter struct {
	UserID            *uuid.UUID
	ActionSubstr      string
	ResourceType      string
	SortDesc          bool
	FromTS, ToTS      *time.Time
	Limit, Offset     int64
}

// InsertAuditLogRow appends one row to audit_logs.
func InsertAuditLogRow(ctx context.Context, pool *pgxpool.Pool, row models.AuditLog) error {
	if pool == nil {
		return errors.New("pool is required")
	}
	details := row.DetailsJSON
	if len(details) == 0 {
		details = []byte("{}")
	}
	var uid any
	if row.UserID == nil || *row.UserID == uuid.Nil {
		uid = nil
	} else {
		uid = *row.UserID
	}
	rt := strings.TrimSpace(row.ResourceType)
	var ip any
	if row.IPAddress != nil {
		s := strings.TrimSpace(*row.IPAddress)
		if s != "" {
			ip = s
		}
	}
	_, err := pool.Exec(ctx, `
INSERT INTO audit_logs (
  user_id, action, resource_type, resource_id, target_asset_id, details, ip_address
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
`, uid, strings.TrimSpace(row.Action), rt, row.ResourceID, row.TargetAssetID, details, ip)
	return err
}

// CountAuditLogs returns the total matching rows for filters.
func CountAuditLogs(ctx context.Context, pool *pgxpool.Pool, f AuditLogFilter) (int64, error) {
	if pool == nil {
		return 0, errors.New("pool is required")
	}
	args := auditFilterArgs(f)
	var total int64
	err := pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM audit_logs a
WHERE ($1::uuid IS NULL OR a.user_id = $1)
  AND ($2::text = '' OR a.action ILIKE '%' || $2 || '%')
  AND ($3::text = '' OR a.resource_type = $3)
  AND ($4::timestamptz IS NULL OR a.created_at >= $4)
  AND ($5::timestamptz IS NULL OR a.created_at < $5)
`, args...).Scan(&total)
	return total, err
}

func auditFilterArgs(f AuditLogFilter) []any {
	rt := strings.TrimSpace(f.ResourceType)
	if rt != "" {
		return []any{f.UserID, f.ActionSubstr, rt, f.FromTS, f.ToTS}
	}
	return []any{f.UserID, f.ActionSubstr, "", f.FromTS, f.ToTS}
}

// ListAuditLogs returns a page of audit rows with filters.
func ListAuditLogs(ctx context.Context, pool *pgxpool.Pool, f AuditLogFilter) ([]models.AuditLog, error) {
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	order := "ASC"
	if f.SortDesc {
		order = "DESC"
	}
	q := fmt.Sprintf(`
SELECT a.id, a.created_at, a.user_id, a.action, a.resource_type, a.resource_id, a.target_asset_id, a.details, a.ip_address
FROM audit_logs a
WHERE ($1::uuid IS NULL OR a.user_id = $1)
  AND ($2::text = '' OR a.action ILIKE '%%' || $2 || '%%')
  AND ($3::text = '' OR a.resource_type = $3)
  AND ($4::timestamptz IS NULL OR a.created_at >= $4)
  AND ($5::timestamptz IS NULL OR a.created_at < $5)
ORDER BY a.created_at %s
LIMIT $6 OFFSET $7
`, order)
	args := auditFilterArgs(f)
	args = append(args, f.Limit, f.Offset)
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AuditLog
	for rows.Next() {
		var row models.AuditLog
		if err := rows.Scan(
			&row.ID, &row.CreatedAt, &row.UserID, &row.Action,
			&row.ResourceType, &row.ResourceID, &row.TargetAssetID, &row.DetailsJSON, &row.IPAddress,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ParseAuditLimit sanitizes limit/offset query params.
func ParseAuditLimit(s string, max int64) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid")
	}
	if n > max {
		return max, nil
	}
	return n, nil
}

// AuditLogUsernames loads usernames for audit rows (keyed by user id).
func AuditLogUsernames(ctx context.Context, pool *pgxpool.Pool, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string)
	if pool == nil || len(userIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `SELECT id, username FROM users WHERE id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// AuditLogHumanIDs loads assets.human_id for target_asset_id values present in rows.
func AuditLogHumanIDs(ctx context.Context, pool *pgxpool.Pool, assetIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string)
	if pool == nil || len(assetIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `SELECT id, human_id FROM assets WHERE id = ANY($1)`, assetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var hid string
		if err := rows.Scan(&id, &hid); err != nil {
			return nil, err
		}
		out[id] = hid
	}
	return out, rows.Err()
}
