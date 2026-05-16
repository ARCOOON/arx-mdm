package alerting

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"
	"github.com/ARCOOON/arx-mdm/internal/notifications"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	offlineBuiltinGrace = 5 * time.Minute
	notifyDedupCooldown = time.Hour
	defaultEngineTick   = 30 * time.Second
	fpBuiltinOfflineFmt = "builtin-offline:%s"
	fpRuleOfflineFmt    = "rule-offline:%s:%s"
	fpRuleMetricFmt     = "rule-metric:%s:%s"
)

// Engine periodically evaluates persisted rules and emits outbound notifications via the Dispatcher.
type Engine struct {
	pool       *pgxpool.Pool
	log        *slog.Logger
	dispatcher *notifications.Dispatcher

	tickEvery time.Duration
}

// Dependencies wires alerting runtime requirements.
type Dependencies struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	Dispatcher   *notifications.Dispatcher
	TickInterval time.Duration
}

// NewEngine builds the alerting evaluator.
func NewEngine(d Dependencies) *Engine {
	tick := d.TickInterval
	if tick <= 0 {
		tick = defaultEngineTick
	}
	return &Engine{
		pool:       d.Pool,
		log:        d.Logger,
		dispatcher: d.Dispatcher,
		tickEvery:  tick,
	}
}

// Start evaluates alert rules until ctx terminates.
func (e *Engine) Start(ctx context.Context) {
	t := time.NewTicker(e.tickEvery)
	go func() {
		defer t.Stop()
		e.evaluate(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e.evaluate(ctx)
			}
		}
	}()
}

// OnHeartbeat clears offline fingerprints when telemetry proves the device reached the dashboard.
func (e *Engine) OnHeartbeat(parent context.Context, deviceID uuid.UUID) {
	if e == nil || e.pool == nil || deviceID == uuid.Nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if err := database.ResolveActiveAlertsKindsForDevice(ctx, e.pool, deviceID); err != nil && e.log != nil {
		e.log.Warn("alerting: resolve offline alerts failed", "device_id", deviceID.String(), "err", err)
	}
}

func (e *Engine) evaluate(parent context.Context) {
	if e.pool == nil || e.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 55*time.Second)
	defer cancel()

	if err := e.evaluateBuiltinOffline(ctx); err != nil && e.log != nil {
		e.log.Error("alerting: builtin offline pass failed", "err", err)
	}
	if err := e.evaluateDeviceRules(ctx); err != nil && e.log != nil {
		e.log.Error("alerting: device rule pass failed", "err", err)
	}
}

func (e *Engine) evaluateBuiltinOffline(ctx context.Context) error {
	desired := map[string]struct{}{}
	cutoff := time.Now().UTC().Add(-offlineBuiltinGrace)

	offlineRows, err := database.ScanAssetsOfflineBefore(ctx, e.pool, cutoff)
	if err != nil {
		return err
	}
	for _, asset := range offlineRows {
		fp := fmt.Sprintf(fpBuiltinOfflineFmt, asset.ID.String())
		desired[fp] = struct{}{}
		var secs float64
		switch {
		case asset.LastSeen == nil:
			secs = database.SecondsSinceLastSeen(nil)
		default:
			secs = time.Now().UTC().Sub(asset.LastSeen.UTC()).Seconds()
		}

		title := fmt.Sprintf("%s offline (built-in watchdog)", asset.HumanID)
		body := fmt.Sprintf(
			"Asset %s (%s) has not communicated for at least %.0f seconds.",
			strings.TrimSpace(asset.HumanID),
			strings.TrimSpace(asset.Hostname),
			offlineBuiltinGrace.Seconds(),
		)
		if asset.LastSeen != nil {
			body += fmt.Sprintf(" Last heartbeat at %s.", asset.LastSeen.UTC().Format(time.RFC3339))
		}

		dev := asset.ID
		outcome, err := database.UpsertUnresolvedAlert(ctx, e.pool,
			fp, database.AlertKindBuiltinOffline, &dev, nil,
			"critical",
			title, body,
			map[string]any{
				"asset_id":     asset.ID.String(),
				"human_id":     asset.HumanID,
				"hostname":     asset.Hostname,
				"offline_secs": secs,
			}, notifyDedupCooldown,
		)
		if err != nil {
			return err
		}
		if outcome.ShouldNotify {
			e.dispatcher.Notify(notifications.AlertEvent{
				Type:     notifications.EventDeviceOffline,
				Severity: "critical",
				Title:    title,
				Message:  body,
				Details: map[string]any{
					"fingerprint":     fp,
					"human_id":        asset.HumanID,
					"offline_seconds": math.Round(secs),
				},
			})
		}
	}
	return database.CloseAlertsKindsExcept(ctx, e.pool, database.AlertKindBuiltinOffline, desired)
}

func (e *Engine) evaluateDeviceRules(ctx context.Context) error {
	rules, err := database.ListAlertRulesEnabled(ctx, e.pool)
	if err != nil {
		return err
	}

	desiredOffline := map[string]struct{}{}
	desiredMetric := map[string]struct{}{}

	for _, rule := range rules {
		devices, err := database.ListAssetsForAlerting(ctx, e.pool, rule.TargetDeviceID)
		if err != nil {
			return err
		}
		window := time.Duration(rule.DurationSeconds) * time.Second
		if window <= 0 {
			window = time.Minute
		}

		switch strings.TrimSpace(rule.Metric) {
		case "offline_status":
			e.handleOfflineRule(ctx, rule, devices, desiredOffline)
		default:
			e.handleTelemetryRule(ctx, rule, devices, window, desiredMetric)
		}
	}

	if err := database.CloseAlertsKindsExcept(ctx, e.pool, database.AlertKindRuleOffline, desiredOffline); err != nil {
		return err
	}
	return database.CloseAlertsKindsExcept(ctx, e.pool, database.AlertKindRuleMetric, desiredMetric)
}

func (e *Engine) handleOfflineRule(
	ctx context.Context,
	rule models.AlertRule,
	devices []uuid.UUID,
	out map[string]struct{},
) {
	for _, assetID := range devices {
		human, host, last, err := database.LoadAssetHeartbeatMeta(ctx, e.pool, assetID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			if e.log != nil {
				e.log.Warn("alerting: heartbeat meta lookup failed", "asset_id", assetID.String(), "err", err)
			}
			continue
		}
		sec := database.SecondsSinceLastSeen(last)
		if !compareFloat(strings.TrimSpace(rule.Operator), sec, rule.Threshold) {
			continue
		}

		fp := fmt.Sprintf(fpRuleOfflineFmt, rule.ID.String(), assetID.String())
		out[fp] = struct{}{}
		title := fmt.Sprintf("[%s] %s breached offline policy", strings.ToUpper(rule.Severity), strings.TrimSpace(human))
		message := fmt.Sprintf(
			"Rule %q reports asset %s has been unreachable for %.0f seconds (threshold %.f, operator %s).",
			rule.Name,
			strings.TrimSpace(fmt.Sprintf("%s %s", human, host)),
			sec,
			rule.Threshold,
			strings.TrimSpace(rule.Operator),
		)

		ruleIDCopy := rule.ID
		device := assetID
		outcome, err := database.UpsertUnresolvedAlert(ctx, e.pool,
			fp, database.AlertKindRuleOffline, &device, &ruleIDCopy,
			strings.TrimSpace(rule.Severity), title, message,
			map[string]any{"rule_id": rule.ID.String(), "asset_id": assetID.String(), "offline_seconds": sec},
			notifyDedupCooldown,
		)
		if err != nil {
			if e.log != nil {
				e.log.Error("alerting: upsert offline rule alert", "rule_id", rule.ID.String(), "err", err)
			}
			continue
		}
		if outcome.ShouldNotify {
			e.dispatcher.Notify(notifications.AlertEvent{
				Type:     notifications.EventRuleOffline,
				Severity: strings.TrimSpace(rule.Severity),
				Title:    title,
				Message:  message,
				Details: map[string]any{
					"rule_id":  rule.ID.String(),
					"asset_id": assetID.String(),
				},
			})
		}
	}
}

func (e *Engine) handleTelemetryRule(
	ctx context.Context,
	rule models.AlertRule,
	devices []uuid.UUID,
	window time.Duration,
	out map[string]struct{},
) {
	for _, assetID := range devices {
		sample, err := database.LoadAggregatedMetricInWindow(ctx, e.pool, assetID, rule.Metric, window)
		if err != nil || sample.SampleRows == 0 {
			continue
		}
		applied := sanitizeMetricSample(rule.Metric, sample.AvgMetric)
		if !compareFloat(strings.TrimSpace(rule.Operator), applied, rule.Threshold) {
			continue
		}

		fp := fmt.Sprintf(fpRuleMetricFmt, rule.ID.String(), assetID.String())
		out[fp] = struct{}{}

		title := fmt.Sprintf("[%s] %s triggered", strings.ToUpper(rule.Severity), rule.Name)
		message := fmt.Sprintf(
			"Metric %s averaged %.2f over %s (threshold %.2f, operator %s, samples %d).",
			strings.TrimSpace(rule.Metric),
			applied,
			window.String(),
			rule.Threshold,
			strings.TrimSpace(rule.Operator),
			sample.SampleRows,
		)

		human := ""
		host := ""
		if metaHuman, metaHost, _, mErr := database.LoadAssetHeartbeatMeta(ctx, e.pool, assetID); mErr == nil {
			human, host = metaHuman, metaHost
		}

		ruleIDCopy := rule.ID
		device := assetID
		outcome, err := database.UpsertUnresolvedAlert(ctx, e.pool,
			fp, database.AlertKindRuleMetric, &device, &ruleIDCopy,
			strings.TrimSpace(rule.Severity), title, message,
			map[string]any{
				"rule_id":   rule.ID.String(),
				"asset_id":  assetID.String(),
				"metric":    strings.TrimSpace(rule.Metric),
				"window":    window.String(),
				"avg_value": applied,
				"human_id":  human,
				"hostname":  host,
				"samples":   sample.SampleRows,
			}, notifyDedupCooldown,
		)
		if err != nil {
			if e.log != nil {
				e.log.Error("alerting: upsert metric alert", "rule_id", rule.ID.String(), "err", err)
			}
			continue
		}
		if outcome.ShouldNotify {
			e.dispatcher.Notify(notifications.AlertEvent{
				Type:     notifications.EventRuleMetric,
				Severity: strings.TrimSpace(rule.Severity),
				Title:    title,
				Message:  message,
				Details: map[string]any{
					"rule_id":  rule.ID.String(),
					"asset_id": assetID.String(),
					"avg":      applied,
				},
			})
		}
	}
}

func sanitizeMetricSample(metric string, v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	switch strings.TrimSpace(metric) {
	case "cpu_usage", "ram_usage_percent", "disk_usage_percent":
		if v < 0 {
			return 0
		}
		return v
	default:
		return v
	}
}

func compareFloat(operator string, value, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	case "==":
		return almostEqual(value, threshold)
	default:
		return false
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6
}
