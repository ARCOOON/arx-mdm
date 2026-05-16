package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NextIncidentHumanNumber allocates the next canonical INCXXXXXXXX display number from incident_seq.
func NextIncidentHumanNumber(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	if pool == nil {
		return "", errors.New("database: pool is required")
	}
	var n int64
	if err := pool.QueryRow(ctx, `SELECT nextval('incident_seq'::regclass)`).Scan(&n); err != nil {
		return "", fmt.Errorf("database: allocate incident seq: %w", err)
	}
	return fmt.Sprintf("INC%07d", n), nil
}

func allocateIncidentNumTx(ctx context.Context, tx pgx.Tx) (string, error) {
	var n int64
	if err := tx.QueryRow(ctx, `SELECT nextval('incident_seq'::regclass)`).Scan(&n); err != nil {
		return "", fmt.Errorf("database: allocate incident seq: %w", err)
	}
	return fmt.Sprintf("INC%07d", n), nil
}

// UpsertIncidentForCriticalFingerprint creates or refreshes an auto incident keyed by alert fingerprint.
func UpsertIncidentForCriticalFingerprint(
	ctx context.Context,
	pool *pgxpool.Pool,
	fingerprint string,
	deviceID uuid.UUID,
	title, alertMessage string,
	severity string,
) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" || deviceID == uuid.Nil {
		return errors.New("database: fingerprint and device id required")
	}
	impact := int16(2)
	urgency := int16(2)
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		impact = 1
		urgency = 1
	case "warning":
		impact = 2
		urgency = 1
	}
	sla := 72 * time.Hour
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		sla = 4 * time.Hour
	case "warning":
		sla = 24 * time.Hour
	}
	slaDue := time.Now().UTC().Add(sla)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var incidentID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT id FROM incidents WHERE source_alert_fingerprint = $1 LIMIT 1 FOR UPDATE
`, fp).Scan(&incidentID)
	if errors.Is(err, pgx.ErrNoRows) {
		incNum, nerr := allocateIncidentNumTx(ctx, tx)
		if nerr != nil {
			return nerr
		}
		openNote := map[string]any{
			"ts":                   time.Now().UTC().Format(time.RFC3339),
			"author_type":          "system",
			"kind":                 "alert_open",
			"text":                 fmt.Sprintf("%s · %s", strings.TrimSpace(title), strings.TrimSpace(alertMessage)),
			"fingerprint":          fp,
			"active_alert_summary": strings.TrimSpace(alertMessage),
			"cm_device_id":         deviceID.String(),
		}
		openBlob, mErr := json.Marshal(openNote)
		if mErr != nil {
			return mErr
		}
		_, ierr := tx.Exec(ctx, `
INSERT INTO incidents (
  incident_number, caller_id, assigned_to,
  cmdb_ci, state, impact, urgency,
  short_description, work_notes,
  sla_due, source_alert_fingerprint
) VALUES (
  $1, NULL, NULL,
  $2, 'new', $3, $4,
  left($5, 500),
  jsonb_build_array($6::jsonb),
  $7, $8
)
`, incNum, deviceID, impact, urgency, strings.TrimSpace(title),
			string(openBlob),
			slaDue, fp)
		if ierr != nil {
			return fmt.Errorf("database: insert incident: %w", ierr)
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("database: lookup incident fingerprint: %w", err)
	}

	repeatNote := map[string]any{
		"ts":           time.Now().UTC().Format(time.RFC3339),
		"author_type":  "system",
		"kind":         "alert_repeat",
		"text":         fmt.Sprintf("Alert re-triggered: %s", strings.TrimSpace(alertMessage)),
		"fingerprint":  fp,
		"short_title":  strings.TrimSpace(title),
		"cm_device_id": deviceID.String(),
	}
	reBlob, mErr := json.Marshal(repeatNote)
	if mErr != nil {
		return mErr
	}
	tag, uerr := tx.Exec(ctx, `
UPDATE incidents SET
  updated_at = now(),
  sla_due = $2,
  short_description = left($3, 500),
  impact = $4,
  urgency = $5,
  cmdb_ci = COALESCE(cmdb_ci, $6),
  state = CASE
    WHEN state IN ('resolved', 'closed') THEN 'in_progress'
    ELSE state
  END,
  work_notes = COALESCE(work_notes, '[]'::jsonb) || jsonb_build_array($1::jsonb)
WHERE id = $7
`, string(reBlob), slaDue,
		strings.TrimSpace(title), impact, urgency, deviceID, incidentID)
	if uerr != nil {
		return uerr
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("database: incident fingerprint update missed row")
	}
	return tx.Commit(ctx)
}

// AutoResolveIncidentsForAlertFingerprints appends recovery notes and marks still-active incidents resolved when safe.
func AutoResolveIncidentsForAlertFingerprints(ctx context.Context, pool *pgxpool.Pool, fingerprints []string) error {
	if pool == nil || len(fingerprints) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fingerprints))
	for _, fp := range fingerprints {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		recovery := map[string]any{
			"ts":          time.Now().UTC().Format(time.RFC3339),
			"author_type": "system",
			"kind":        "alert_recovery",
			"text":        fmt.Sprintf("Monitoring detected recovery for alert fingerprint %s. Incident resolved automatically.", fp),
			"fingerprint": fp,
		}
		blob, err := json.Marshal(recovery)
		if err != nil {
			return err
		}
		_, err = pool.Exec(ctx, `
UPDATE incidents SET
  work_notes = COALESCE(work_notes, '[]'::jsonb) || jsonb_build_array($1::jsonb),
  updated_at = now(),
  state = CASE
    WHEN state IN ('new', 'in_progress', 'on_hold') THEN 'resolved'
    ELSE state
  END
WHERE source_alert_fingerprint = $2 AND state <> 'closed'
`, string(blob), fp)
		if err != nil {
			return fmt.Errorf("database: auto-resolve incidents: %w", err)
		}
	}
	return nil
}

// AppendIncidentWorkNoteJSON merges one JSON note object into incidents.work_notes.
func AppendIncidentWorkNoteJSON(ctx context.Context, pool *pgxpool.Pool, incidentID uuid.UUID, note json.RawMessage) error {
	if pool == nil || incidentID == uuid.Nil {
		return errors.New("database: invalid append")
	}
	if len(note) == 0 {
		return errors.New("database: empty work note")
	}
	tag, err := pool.Exec(ctx, `
UPDATE incidents
SET work_notes = COALESCE(work_notes, '[]'::jsonb) || jsonb_build_array($1::jsonb),
    updated_at = now()
WHERE id = $2
`, string(note), incidentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// IncidentCMDBSnapshot aggregates CMDB-oriented fields plus recent telemetry-derived metrics.
type IncidentCMDBSnapshot struct {
	DeviceID           uuid.UUID
	HumanID            string
	Hostname           *string
	OperationalStatus  string
	CostCenter         string
	Location           string
	CertSerial         *string
	LastSeen           *time.Time
	CpuUsagePercent    *float64
	RAMPercent         *float64
	DiskPercent        *float64
	LatestMetricsAt    *time.Time
	RecentCommandTypes []string
}

// LoadIncidentCMDBSnapshot pulls device row plus last metrics sample when available.
func LoadIncidentCMDBSnapshot(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID) (*IncidentCMDBSnapshot, error) {
	if pool == nil || deviceID == uuid.Nil {
		return nil, errors.New("database: snapshot args")
	}
	row := pool.QueryRow(ctx, `
SELECT id,
       trim(human_id),
       hostname,
       operational_status,
       cost_center,
       location,
       cert_serial,
       last_seen,
       dm.cpu_usage,
       dm.ram_used,
       dm.ram_total,
       dm.disk_used,
       dm.disk_total,
       dm.created_at
FROM assets a
LEFT JOIN LATERAL (
  SELECT cpu_usage,
         ram_used,
         ram_total,
         disk_used,
         disk_total,
         created_at
  FROM device_metrics
  WHERE device_id = a.id
  ORDER BY created_at DESC
  LIMIT 1
) dm ON true
WHERE a.id = $1
`, deviceID)

	var snap IncidentCMDBSnapshot
	var hostname, cert *string
	var lastSeen *time.Time
	var cpu *float64
	var ramUsed, ramTotal *int64
	var diskUsed, diskTotal *int64
	var metricsAt *time.Time
	err := row.Scan(
		&snap.DeviceID,
		&snap.HumanID,
		&hostname,
		&snap.OperationalStatus,
		&snap.CostCenter,
		&snap.Location,
		&cert,
		&lastSeen,
		&cpu,
		&ramUsed,
		&ramTotal,
		&diskUsed,
		&diskTotal,
		&metricsAt,
	)
	snap.Hostname = hostname
	snap.CertSerial = cert
	snap.LastSeen = lastSeen
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("database: cmdb snapshot: %w", err)
	}
	if cpu != nil && metricsAt != nil {
		snap.CpuUsagePercent = cpu
		snap.LatestMetricsAt = metricsAt
	}
	if ramTotal != nil && *ramTotal > 0 && ramUsed != nil && metricsAt != nil {
		v := 100.0 * float64(*ramUsed) / float64(*ramTotal)
		snap.RAMPercent = &v
	}
	if diskTotal != nil && *diskTotal > 0 && diskUsed != nil && metricsAt != nil {
		v := 100.0 * float64(*diskUsed) / float64(*diskTotal)
		snap.DiskPercent = &v
	}

	typesRows, terr := pool.Query(ctx, `
SELECT command_type FROM device_commands
WHERE device_id = $1
ORDER BY created_at DESC
LIMIT 12
`, deviceID)
	if terr == nil {
		defer typesRows.Close()
		for typesRows.Next() {
			var ct string
			if scanErr := typesRows.Scan(&ct); scanErr == nil && strings.TrimSpace(ct) != "" {
				snap.RecentCommandTypes = append(snap.RecentCommandTypes, strings.TrimSpace(ct))
			}
		}
	}
	return &snap, nil
}
