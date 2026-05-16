package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
)

// Hub is the C2 dispatch surface used by automations (implemented by *ws.Hub).
type Hub interface {
	DispatchJSONByHumanID(ctx context.Context, pool *pgxpool.Pool, humanID string, payload any) error
}

// Deps wires the automation scheduler.
type Deps struct {
	Pool           *pgxpool.Pool
	Hub            Hub
	Logger         *slog.Logger
	ReloadInterval time.Duration
}

type automationRow struct {
	ID            uuid.UUID
	Name          string
	CronSchedule  string
	ActionType    string
	TargetOS      *string
	TargetAssetID *uuid.UUID
	PayloadJSON   []byte
	IsActive      bool
}

type Scheduler struct {
	deps Deps
	mu   sync.Mutex
	cr   *cron.Cron
}

// Run loads active automations on an interval, registers cron jobs, and dispatches to agents until ctx is cancelled.
func Run(ctx context.Context, deps Deps) {
	if deps.Pool == nil || deps.Hub == nil {
		return
	}
	iv := deps.ReloadInterval
	if iv <= 0 {
		iv = time.Minute
	}
	s := &Scheduler{deps: deps}
	s.reload(ctx)
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.stopCron()
			return
		case <-t.C:
			s.reload(ctx)
		}
	}
}

func (s *Scheduler) stopCron() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cr == nil {
		return
	}
	ctx := s.cr.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
	}
	s.cr = nil
}

func (s *Scheduler) reload(ctx context.Context) {
	if s.deps.Logger != nil {
		s.deps.Logger.Debug("scheduler reloading automations")
	}
	rows, err := s.loadActive(ctx)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler load automations failed", "err", err)
		}
		return
	}

	s.mu.Lock()
	if s.cr != nil {
		stopCtx := s.cr.Stop()
		select {
		case <-stopCtx.Done():
		case <-time.After(30 * time.Second):
		}
	}
	s.cr = cron.New(
		cron.WithLocation(time.UTC),
		cron.WithChain(cron.Recover(cron.DiscardLogger)),
	)
	for _, row := range rows {
		row := row
		sched, err := parseCronSpec(row.CronSchedule)
		if err != nil {
			if s.deps.Logger != nil {
				s.deps.Logger.Warn("scheduler skip automation: invalid cron",
					"automation_id", row.ID.String(),
					"cron_schedule", row.CronSchedule,
					"err", err,
				)
			}
			continue
		}
		s.cr.Schedule(sched, cron.FuncJob(func() {
			s.dispatchAutomation(context.Background(), row.ID)
		}))
	}
	s.cr.Start()
	s.mu.Unlock()
}

// ValidateCronSchedule checks that spec can be scheduled (5-field standard or optional-seconds / descriptors).
func ValidateCronSchedule(spec string) error {
	_, err := parseCronSpec(spec)
	return err
}

func parseCronSpec(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty cron expression")
	}
	if sched, err := cron.ParseStandard(spec); err == nil {
		return sched, nil
	}
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return parser.Parse(spec)
}

func (s *Scheduler) loadActive(ctx context.Context) ([]automationRow, error) {
	qctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows, err := s.deps.Pool.Query(qctx, `
SELECT id, name, cron_schedule, action_type, target_os, target_asset_id, payload_json, is_active
FROM automations
WHERE is_active = true
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []automationRow
	for rows.Next() {
		var r automationRow
		if err := rows.Scan(&r.ID, &r.Name, &r.CronSchedule, &r.ActionType, &r.TargetOS, &r.TargetAssetID, &r.PayloadJSON, &r.IsActive); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Scheduler) dispatchAutomation(ctx context.Context, automationID uuid.UUID) {
	dctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var row automationRow
	err := s.deps.Pool.QueryRow(dctx, `
SELECT id, name, cron_schedule, action_type, target_os, target_asset_id, payload_json, is_active
FROM automations WHERE id = $1
`, automationID).Scan(
		&row.ID, &row.Name, &row.CronSchedule, &row.ActionType, &row.TargetOS, &row.TargetAssetID, &row.PayloadJSON, &row.IsActive,
	)
	if err != nil {
		if s.deps.Logger != nil && !errors.Is(err, pgx.ErrNoRows) {
			s.deps.Logger.Warn("scheduler automation fetch failed", "automation_id", automationID.String(), "err", err)
		}
		return
	}
	if !row.IsActive {
		return
	}

	humanIDs, err := s.resolveTargetHumanIDs(dctx, row)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler resolve targets failed", "automation_id", automationID.String(), "err", err)
		}
		return
	}
	if len(humanIDs) == 0 {
		if s.deps.Logger != nil {
			s.deps.Logger.Info("scheduler no eligible targets", "automation_id", automationID.String(), "name", row.Name)
		}
		return
	}

	action := strings.TrimSpace(strings.ToLower(row.ActionType))
	for _, hid := range humanIDs {
		switch action {
		case "shutdown":
			s.dispatchShutdown(dctx, row, hid)
		case "deploy_package":
			s.dispatchDeploy(dctx, row, hid)
		default:
			if s.deps.Logger != nil {
				s.deps.Logger.Warn("scheduler unknown action", "action", row.ActionType)
			}
		}
	}
}

func (s *Scheduler) resolveTargetHumanIDs(ctx context.Context, row automationRow) ([]string, error) {
	if row.TargetAssetID != nil {
		var hid string
		err := s.deps.Pool.QueryRow(ctx, `
SELECT human_id FROM assets
WHERE id = $1 AND cert_serial IS NOT NULL AND trim(cert_serial) <> ''
`, *row.TargetAssetID).Scan(&hid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		return []string{strings.TrimSpace(hid)}, nil
	}
	if row.TargetOS == nil {
		return nil, errors.New("target_os and target_asset_id unset")
	}
	os := strings.TrimSpace(strings.ToLower(*row.TargetOS))
	rows, err := s.deps.Pool.Query(ctx, `
SELECT human_id FROM assets
WHERE os_type = $1 AND cert_serial IS NOT NULL AND trim(cert_serial) <> ''
`, os)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var hid string
		if err := rows.Scan(&hid); err != nil {
			return nil, err
		}
		if t := strings.TrimSpace(hid); t != "" {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func (s *Scheduler) dispatchShutdown(ctx context.Context, row automationRow, humanID string) {
	err := s.deps.Hub.DispatchJSONByHumanID(ctx, s.deps.Pool, humanID, map[string]string{"action": "shutdown"})
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler shutdown dispatch failed",
				"automation_id", row.ID.String(), "human_id", humanID, "err", err)
		}
		return
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Info("scheduler shutdown dispatched",
			"automation_id", row.ID.String(), "human_id", humanID)
	}
	s.auditDispatch(ctx, row, humanID, "automation.shutdown", map[string]any{
		"automation_id":   row.ID.String(),
		"automation_name": row.Name,
		"target_human_id": humanID,
	})
}

func (s *Scheduler) dispatchDeploy(ctx context.Context, row automationRow, humanID string) {
	var payload map[string]any
	if len(row.PayloadJSON) > 0 {
		if err := json.Unmarshal(row.PayloadJSON, &payload); err != nil {
			if s.deps.Logger != nil {
				s.deps.Logger.Warn("scheduler deploy invalid payload_json", "automation_id", row.ID.String(), "err", err)
			}
			return
		}
	}
	rawPkg, _ := payload["package_id"].(string)
	pkgID, err := uuid.Parse(strings.TrimSpace(rawPkg))
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy missing package_id", "automation_id", row.ID.String())
		}
		return
	}
	op := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["operation"])))
	if op == "" {
		op = "install"
	}
	if op != "install" && op != "uninstall" {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy invalid operation", "operation", op)
		}
		return
	}

	var assetID uuid.UUID
	if err := s.deps.Pool.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, humanID).Scan(&assetID); err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy asset lookup failed", "human_id", humanID, "err", err)
		}
		return
	}

	var pkg models.Package
	if err := s.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), type, COALESCE(install_cmd, '')
FROM packages WHERE id = $1
`, pkgID).Scan(&pkg.ID, &pkg.Name, &pkg.Version, &pkg.Type, &pkg.InstallCmd); err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy package load failed", "package_id", pkgID.String(), "err", err)
		}
		return
	}

	var depID uuid.UUID
	if err := s.deps.Pool.QueryRow(ctx, `
INSERT INTO deployments (asset_id, package_id, status)
VALUES ($1, $2, 'pending')
RETURNING id
`, assetID, pkg.ID).Scan(&depID); err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy insert failed", "err", err)
		}
		return
	}

	dispatchPayload := map[string]any{
		"action":        "deploy_package",
		"deployment_id": depID.String(),
		"operation":     op,
		"package_type":  pkg.Type,
		"name":          pkg.Name,
		"version":       pkg.Version,
		"install_cmd":   pkg.InstallCmd,
	}
	if err := s.deps.Hub.DispatchJSONByHumanID(ctx, s.deps.Pool, humanID, dispatchPayload); err != nil {
		_, _ = s.deps.Pool.Exec(ctx, `
UPDATE deployments SET status = 'failed', error_message = $2, updated_at = now() WHERE id = $1
`, depID, err.Error())
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("scheduler deploy dispatch failed", "human_id", humanID, "err", err)
		}
		return
	}
	_, _ = s.deps.Pool.Exec(ctx, `UPDATE deployments SET status = 'dispatched', updated_at = now() WHERE id = $1`, depID)
	if s.deps.Logger != nil {
		s.deps.Logger.Info("scheduler deploy dispatched",
			"automation_id", row.ID.String(), "human_id", humanID, "deployment_id", depID.String())
	}
	s.auditDispatch(ctx, row, humanID, "automation.deploy_package", map[string]any{
		"automation_id":   row.ID.String(),
		"automation_name": row.Name,
		"target_human_id": humanID,
		"deployment_id":   depID.String(),
		"package_id":      pkg.ID.String(),
	})
}

func (s *Scheduler) auditDispatch(ctx context.Context, row automationRow, humanID, action string, details map[string]any) {
	actx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var assetID *uuid.UUID
	var id uuid.UUID
	err := s.deps.Pool.QueryRow(actx, `SELECT id FROM assets WHERE human_id = $1 LIMIT 1`, humanID).Scan(&id)
	if err == nil {
		assetID = &id
	}
	if err := auth.InsertAuditRecord(actx, s.deps.Pool, auth.AuditRecord{
		UserID:        uuid.Nil,
		Action:        action,
		ResourceType:  "device",
		ResourceID:    assetID,
		TargetAssetID: assetID,
		Details:       details,
	}); err != nil && s.deps.Logger != nil {
		s.deps.Logger.Warn("scheduler audit log failed", "err", err, "action", action)
	}
}
