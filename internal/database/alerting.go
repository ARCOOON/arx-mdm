package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	AlertKindBuiltinOffline = "builtin_offline"
	AlertKindRuleOffline    = "rule_offline"
	AlertKindRuleMetric     = "rule_metric"
)

// ListAlertRules returns all alert rules newest first.
func ListAlertRules(ctx context.Context, pool *pgxpool.Pool) ([]models.AlertRule, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	rows, err := pool.Query(ctx, `
SELECT id, name, target_type, metric, operator, threshold,
       EXTRACT(EPOCH FROM duration)::bigint,
       severity, is_enabled, target_device_id, created_at, updated_at
FROM alert_rules
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, fmt.Errorf("database: list alert_rules: %w", err)
	}
	defer rows.Close()

	var out []models.AlertRule
	for rows.Next() {
		var r models.AlertRule
		if err := rows.Scan(
			&r.ID,
			&r.Name,
			&r.TargetType,
			&r.Metric,
			&r.Operator,
			&r.Threshold,
			&r.DurationSeconds,
			&r.Severity,
			&r.IsEnabled,
			&r.TargetDeviceID,
			&r.CreatedAt,
			&r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("database: scan alert_rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertAlertRule inserts a rule; duration_seconds is converted to PostgreSQL INTERVAL.
func InsertAlertRule(ctx context.Context, pool *pgxpool.Pool, r models.AlertRule) (uuid.UUID, error) {
	if pool == nil {
		return uuid.Nil, errors.New("database: pool is required")
	}
	if r.DurationSeconds <= 0 {
		return uuid.Nil, errors.New("database: duration_seconds must be positive")
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return uuid.Nil, errors.New("database: name is required")
	}
	var id uuid.UUID
	err := pool.QueryRow(ctx, `
INSERT INTO alert_rules (
  name, target_type, metric, operator, threshold, duration,
  severity, is_enabled, target_device_id
) VALUES ($1,$2,$3,$4,$5,($6::bigint * INTERVAL '1 second'),$7,$8,$9)
RETURNING id
`, name, strings.TrimSpace(r.TargetType), strings.TrimSpace(r.Metric),
		strings.TrimSpace(r.Operator), r.Threshold, r.DurationSeconds,
		strings.TrimSpace(r.Severity), r.IsEnabled, r.TargetDeviceID).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("database: insert alert_rule: %w", err)
	}
	return id, nil
}

// UpdateAlertRule updates mutable columns.
func UpdateAlertRule(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, r models.AlertRule) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	if r.DurationSeconds <= 0 {
		return errors.New("database: duration_seconds must be positive")
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		return errors.New("database: name is required")
	}
	tag, err := pool.Exec(ctx, `
UPDATE alert_rules
SET name = $2,
    target_type = $3,
    metric = $4,
    operator = $5,
    threshold = $6,
    duration = ($7::bigint * INTERVAL '1 second'),
    severity = $8,
    is_enabled = $9,
    target_device_id = $10,
    updated_at = now()
WHERE id = $1
`, id, name, strings.TrimSpace(r.TargetType), strings.TrimSpace(r.Metric),
		strings.TrimSpace(r.Operator), r.Threshold, r.DurationSeconds,
		strings.TrimSpace(r.Severity), r.IsEnabled, r.TargetDeviceID)
	if err != nil {
		return fmt.Errorf("database: update alert_rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteAlertRule removes a rule.
func DeleteAlertRule(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	tag, err := pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("database: delete alert_rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// LoadAlertRule loads a persisted rule row.
func LoadAlertRule(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (models.AlertRule, error) {
	var zero models.AlertRule
	if pool == nil {
		return zero, errors.New("database: pool is required")
	}
	var r models.AlertRule
	err := pool.QueryRow(ctx, `
SELECT id, name, target_type, metric, operator, threshold,
       EXTRACT(EPOCH FROM duration)::bigint,
       severity, is_enabled, target_device_id, created_at, updated_at
FROM alert_rules WHERE id = $1
`, id).Scan(
		&r.ID,
		&r.Name,
		&r.TargetType,
		&r.Metric,
		&r.Operator,
		&r.Threshold,
		&r.DurationSeconds,
		&r.Severity,
		&r.IsEnabled,
		&r.TargetDeviceID,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		return zero, err
	}
	return r, nil
}

// AlertRuleMetricSample is an aggregated telemetry window sample.
type AlertRuleMetricSample struct {
	AvgMetric    float64
	SampleRows   int64
	WindowStarts time.Time
	WindowEnds   time.Time
}

// LoadAggregatedMetricInWindow computes the average metric in [now-window, now].
func LoadAggregatedMetricInWindow(
	ctx context.Context,
	pool *pgxpool.Pool,
	deviceID uuid.UUID,
	metricKey string,
	window time.Duration,
) (AlertRuleMetricSample, error) {
	var zero AlertRuleMetricSample
	if pool == nil {
		return zero, errors.New("database: pool is required")
	}
	if window <= 0 {
		return zero, errors.New("database: window must be positive")
	}
	cutoff := time.Now().UTC().Add(-window)
	var expr string
	switch strings.TrimSpace(metricKey) {
	case "cpu_usage":
		expr = "cpu_usage"
	case "ram_usage_percent":
		expr = `CASE WHEN ram_total > 0 THEN (100.0 * ram_used::float8 / ram_total::float8) END`
	case "disk_usage_percent":
		expr = `CASE WHEN disk_total > 0 THEN (100.0 * disk_used::float8 / disk_total::float8) END`
	default:
		return zero, fmt.Errorf("database: unknown metric %q", metricKey)
	}
	row := pool.QueryRow(ctx, fmt.Sprintf(`
SELECT
  COALESCE(AVG(%s)::float8, 0)::float8,
  COUNT(*)::bigint,
  MIN(created_at),
  MAX(created_at)
FROM device_metrics
WHERE device_id = $1 AND created_at >= $2
`, expr), deviceID, cutoff)
	err := row.Scan(&zero.AvgMetric, &zero.SampleRows, &zero.WindowStarts, &zero.WindowEnds)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && zero.SampleRows == 0) {
		return AlertRuleMetricSample{}, nil
	}
	if err != nil {
		return zero, fmt.Errorf("database: metric window: %w", err)
	}
	return zero, nil
}

// ListAssetsForAlerting enumerates asset ids optionally filtered by a single device.
func ListAssetsForAlerting(ctx context.Context, pool *pgxpool.Pool, only *uuid.UUID) ([]uuid.UUID, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	var rows pgx.Rows
	var err error
	if only != nil && *only != uuid.Nil {
		rows, err = pool.Query(ctx, `SELECT id FROM assets WHERE id = $1`, only)
	} else {
		rows, err = pool.Query(ctx, `SELECT id FROM assets ORDER BY human_id ASC`)
	}
	if err != nil {
		return nil, fmt.Errorf("database: list assets for alerting: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("database: scan asset id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AssetLastSeenRow is used by the alerting engine for offline semantics.
type AssetLastSeenRow struct {
	ID       uuid.UUID
	HumanID  string
	Hostname string
	LastSeen *time.Time
}

// ScanAssetsOfflineBefore returns assets whose last_seen is older than cutoff (or unset).
func ScanAssetsOfflineBefore(ctx context.Context, pool *pgxpool.Pool, cutoffUTC time.Time) ([]AssetLastSeenRow, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	rows, err := pool.Query(ctx, `
SELECT id, COALESCE(trim(human_id), ''), COALESCE(trim(hostname), ''),
       last_seen
FROM assets
WHERE last_seen IS NULL OR last_seen < $1
`, cutoffUTC)
	if err != nil {
		return nil, fmt.Errorf("database: offline scan: %w", err)
	}
	defer rows.Close()
	var out []AssetLastSeenRow
	for rows.Next() {
		var r AssetLastSeenRow
		if err := rows.Scan(&r.ID, &r.HumanID, &r.Hostname, &r.LastSeen); err != nil {
			return nil, fmt.Errorf("database: scan offline asset: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadAssetHeartbeatMeta loads human_id and hostname plus last_seen for offline rule evaluation.
func LoadAssetHeartbeatMeta(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) (humanID, hostname string, lastSeen *time.Time, err error) {
	if pool == nil {
		return "", "", nil, errors.New("database: pool is required")
	}
	err = pool.QueryRow(ctx, `
SELECT COALESCE(trim(human_id), ''), COALESCE(trim(hostname), ''), last_seen
FROM assets WHERE id = $1
`, assetID).Scan(&humanID, &hostname, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, pgx.ErrNoRows
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("database: load asset meta: %w", err)
	}
	return humanID, hostname, lastSeen, nil
}

// SecondsSinceLastSeen computes how long ago the heartbeat was (Infinity if unseen).
func SecondsSinceLastSeen(last *time.Time) float64 {
	if last == nil {
		return 1e12
	}
	return time.Now().UTC().Sub(last.UTC()).Seconds()
}

// FireAlertOutcome explains whether outbound notification should occur.
type FireAlertOutcome struct {
	AlertID      uuid.UUID
	ShouldNotify bool
}



// UpsertUnresolvedAlert opens or refreshes one active alert fingerprint and computes notify cadence.
func UpsertUnresolvedAlert(
	ctx context.Context,
	pool *pgxpool.Pool,
	fingerprint, alertKind string,
	deviceID *uuid.UUID,
	ruleID *uuid.UUID,
	severity, title, message string,
	details map[string]any,
	reopenNotifyCooldown time.Duration,
) (FireAlertOutcome, error) {
	var zero FireAlertOutcome
	if pool == nil {
		return zero, errors.New("database: pool is required")
	}
	if reopenNotifyCooldown <= 0 {
		reopenNotifyCooldown = time.Hour
	}

	detailBytes := []byte(`{}`)
	if details != nil {
		b, err := json.Marshal(details)
		if err != nil {
			return zero, err
		}
		detailBytes = b
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id uuid.UUID
	var lastNotify *time.Time
	var resolved *time.Time
	err = tx.QueryRow(ctx, `
SELECT id, last_notified_at, resolved_at FROM active_alerts WHERE fingerprint = $1
FOR UPDATE
`, fingerprint).Scan(&id, &lastNotify, &resolved)
	decideNotifyOnOpen := func(reopened bool) bool {
		if reopened {
			return true
		}
		if lastNotify == nil {
			return true
		}
		return time.Since(lastNotify.UTC()) >= reopenNotifyCooldown
	}

	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
INSERT INTO active_alerts (
  fingerprint, alert_kind, device_id, alert_rule_id,
  severity, title, message, details, triggered_at, last_notified_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb, now(), now())
RETURNING id
`,
			fingerprint, alertKind, deviceID, ruleID,
			strings.TrimSpace(severity),
			strings.TrimSpace(title),
			strings.TrimSpace(message),
			string(detailBytes),
		).Scan(&id)
		if err != nil {
			return zero, fmt.Errorf("database: insert active alert: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return zero, err
		}
		return FireAlertOutcome{AlertID: id, ShouldNotify: true}, nil
	}
	if err != nil {
		return zero, fmt.Errorf("database: lock active alert: %w", err)
	}

	reopened := resolved != nil
	if reopened {
		_, err = tx.Exec(ctx, `
UPDATE active_alerts
SET alert_rule_id = $2,
    alert_kind = $3,
    device_id = $4,
    severity = $5,
    title = $6,
    message = $7,
    details = $8::jsonb,
    resolved_at = NULL,
    triggered_at = now(),
    last_notified_at = NULL
WHERE id = $1
`, id, ruleID, alertKind, deviceID,
			strings.TrimSpace(severity),
			strings.TrimSpace(title),
			strings.TrimSpace(message),
			string(detailBytes))
		lastNotify = nil
	} else {
		_, err = tx.Exec(ctx, `
UPDATE active_alerts
SET alert_rule_id = COALESCE($2, alert_rule_id),
    device_id = COALESCE($3, device_id),
    severity = $4,
    title = $5,
    message = $6,
    details = $7::jsonb
WHERE id = $1
`, id, ruleID, deviceID,
			strings.TrimSpace(severity),
			strings.TrimSpace(title),
			strings.TrimSpace(message),
			string(detailBytes))
	}
	if err != nil {
		return zero, fmt.Errorf("database: refresh active alert: %w", err)
	}

	shouldNotify := decideNotifyOnOpen(reopened)
	if shouldNotify {
		_, err = tx.Exec(ctx, `UPDATE active_alerts SET last_notified_at = now() WHERE id = $1`, id)
		if err != nil {
			return zero, fmt.Errorf("database: stamp notify window: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return FireAlertOutcome{AlertID: id, ShouldNotify: shouldNotify}, nil
}

// ResolveActiveAlertsKindsForDevice clears offline rows after a heartbeat.
func ResolveActiveAlertsKindsForDevice(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID) error {
	if pool == nil || deviceID == uuid.Nil {
		return errors.New("database: resolve offline args")
	}
	_, err := pool.Exec(ctx, `
UPDATE active_alerts SET resolved_at = now()
WHERE resolved_at IS NULL
  AND device_id = $1
  AND alert_kind IN ('builtin_offline','rule_offline')
`, deviceID)
	return err
}

// ResolveActiveAlertByID marks one row resolved manually.
func ResolveActiveAlertByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	tag, err := pool.Exec(ctx, `
UPDATE active_alerts SET resolved_at = now() WHERE id = $1 AND resolved_at IS NULL
`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ListActiveAlerts returns recent alerts optionally including resolved rows.
func ListActiveAlerts(ctx context.Context, pool *pgxpool.Pool, limit int, includeResolved bool) ([]models.ActiveAlert, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows pgx.Rows
	var err error
	if includeResolved {
		rows, err = pool.Query(ctx, `
SELECT id, alert_rule_id, fingerprint, alert_kind, device_id, severity,
       title, message, details::text, triggered_at, last_notified_at, resolved_at
FROM active_alerts
ORDER BY (resolved_at IS NULL) DESC, triggered_at DESC
LIMIT $1
`, limit)
	} else {
		rows, err = pool.Query(ctx, `
SELECT id, alert_rule_id, fingerprint, alert_kind, device_id, severity,
       title, message, details::text, triggered_at, last_notified_at, resolved_at
FROM active_alerts
WHERE resolved_at IS NULL
ORDER BY triggered_at DESC
LIMIT $1
`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("database: list active_alerts: %w", err)
	}
	defer rows.Close()
	var out []models.ActiveAlert
	for rows.Next() {
		var a models.ActiveAlert
		var detailStr string
		if err := rows.Scan(
			&a.ID, &a.AlertRuleID, &a.Fingerprint, &a.AlertKind, &a.DeviceID,
			&a.Severity, &a.Title, &a.Message, &detailStr,
			&a.TriggeredAt, &a.LastNotifiedAt, &a.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("database: scan active alert: %w", err)
		}
		a.Details = []byte(detailStr)
		out = append(out, a)
	}
	return out, rows.Err()
}

// CloseAlertsKindsExcept resolves open alerts of alertKind unless their fingerprint stays in desiredFingerprints.
func CloseAlertsKindsExcept(ctx context.Context, pool *pgxpool.Pool, alertKind string, desiredFingerprints map[string]struct{}) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	k := strings.TrimSpace(alertKind)
	if k == "" {
		return errors.New("database: alert kind required")
	}
	if len(desiredFingerprints) == 0 {
		_, err := pool.Exec(ctx, `
UPDATE active_alerts SET resolved_at = now()
WHERE resolved_at IS NULL AND alert_kind = $1
`, k)
		return err
	}
	set := make([]string, 0, len(desiredFingerprints))
	for fp := range desiredFingerprints {
		fp = strings.TrimSpace(fp)
		if fp != "" {
			set = append(set, fp)
		}
	}
	if len(set) == 0 {
		_, err := pool.Exec(ctx, `
UPDATE active_alerts SET resolved_at = now()
WHERE resolved_at IS NULL AND alert_kind = $1
`, k)
		return err
	}
	_, err := pool.Exec(ctx, `
UPDATE active_alerts SET resolved_at = now()
WHERE resolved_at IS NULL
  AND alert_kind = $2
  AND fingerprint <> ALL ($1::text[])
`, set, k)
	return err
}

// NotificationChannelRow is persisted delivery configuration loaded by notifications.Dispatcher.
type NotificationChannelRow struct {
	ID            uuid.UUID
	ChannelType   string
	ConfigJSON    []byte
	SigningSecret string
	IsActive      bool
}

func ListActiveNotificationChannels(ctx context.Context, pool *pgxpool.Pool) ([]NotificationChannelRow, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	rows, err := pool.Query(ctx, `
SELECT id, channel_type, config_json::text, COALESCE(signing_secret,''), is_active
FROM notification_channels WHERE is_active = true ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("database: list notification_channels: %w", err)
	}
	defer rows.Close()

	var out []NotificationChannelRow
	for rows.Next() {
		var r NotificationChannelRow
		var cfg string
		if err := rows.Scan(&r.ID, &r.ChannelType, &cfg, &r.SigningSecret, &r.IsActive); err != nil {
			return nil, err
		}
		r.ConfigJSON = []byte(cfg)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadNotificationChannelByID loads any row for testing delivery.
func LoadNotificationChannelByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*NotificationChannelRow, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	var r NotificationChannelRow
	var cfg string
	err := pool.QueryRow(ctx, `
SELECT id, channel_type, config_json::text, COALESCE(signing_secret,''), is_active
FROM notification_channels WHERE id = $1
`, id).Scan(&r.ID, &r.ChannelType, &cfg, &r.SigningSecret, &r.IsActive)
	r.ConfigJSON = []byte(cfg)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListAlertRulesEnabled returns alerting rules flagged active.
func ListAlertRulesEnabled(ctx context.Context, pool *pgxpool.Pool) ([]models.AlertRule, error) {
	if pool == nil {
		return nil, errors.New("database: pool is required")
	}
	rows, err := pool.Query(ctx, `
SELECT id, name, target_type, metric, operator, threshold,
       EXTRACT(EPOCH FROM duration)::bigint,
       severity, is_enabled, target_device_id, created_at, updated_at
FROM alert_rules
WHERE is_enabled = true AND target_type = 'device'
ORDER BY created_at ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AlertRule
	for rows.Next() {
		var r models.AlertRule
		if err := rows.Scan(
			&r.ID, &r.Name, &r.TargetType, &r.Metric, &r.Operator, &r.Threshold,
			&r.DurationSeconds, &r.Severity, &r.IsEnabled, &r.TargetDeviceID,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
