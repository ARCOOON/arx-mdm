package compliance

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// C2Dispatcher pushes JSON device commands to a connected agent session.
type C2Dispatcher interface {
	DispatchJSON(certSerial string, payload any) bool
}

// EnforcementWire is the agent-reported MDM enforcement posture.
type EnforcementWire struct {
	State          string
	Detail         string
	CriticalFailed bool
}

// EvaluateAfterTelemetry updates compliance from telemetry and may enqueue automatic quarantine.
func EvaluateAfterTelemetry(ctx context.Context, pool *pgxpool.Pool, dispatch C2Dispatcher,
	logger *slog.Logger, assetID uuid.UUID, humanID, certSerial, platformKey string, enf *EnforcementWire,
) error {
	if pool == nil {
		return ErrNilPool
	}
	prev, _ := database.GetAssetCompliance(ctx, pool, assetID)

	status := database.ComplianceStatusEvaluating
	reason := ""

	effective, mergeErr := EffectiveMergedPayload(ctx, pool, assetID, platformKey)
	hasCritical := mergeErr != nil || PayloadCarriesCriticalSecurityControls(effective)

	if enf == nil {
		if prev.Status != "" && prev.Status != database.ComplianceStatusEvaluating {
			return nil
		}
		status = database.ComplianceStatusEvaluating
		reason = ""
	} else {
		st := strings.ToLower(strings.TrimSpace(enf.State))
		switch st {
		case "ok", "success", "applied":
			status = database.ComplianceStatusCompliant
			reason = ""
		case "error", "failed":
			critical := enf.CriticalFailed || hasCritical
			if critical {
				status = database.ComplianceStatusNonCompliant
				reason = strings.TrimSpace(enf.Detail)
				if reason == "" {
					reason = "critical security policy enforcement failed"
				}
			} else {
				status = database.ComplianceStatusCompliant
				reason = ""
			}
		default:
			if hasCritical {
				status = database.ComplianceStatusEvaluating
				reason = strings.TrimSpace(enf.Detail)
			} else {
				status = database.ComplianceStatusCompliant
				reason = ""
			}
		}
	}

	if err := database.SetAssetCompliance(ctx, pool, assetID, status, reason); err != nil {
		return err
	}

	auto, err := database.TenantAutoQuarantine(ctx, pool)
	if err != nil && logger != nil {
		logger.Warn("compliance: tenant auto-quarantine read failed", "err", err)
		auto = false
	}

	if auto && status == database.ComplianceStatusNonCompliant &&
		prev.Status != database.ComplianceStatusNonCompliant &&
		certSerial != "" {
		payload := BuildQuarantinePayloadJSON(true)
		cmd, ierr := database.InsertDeviceCommand(ctx, pool, assetID, models.DeviceCommandTypeQuarantine, payload, nil)
		if ierr != nil {
			if logger != nil {
				logger.Warn("compliance: auto quarantine enqueue failed", "err", ierr, "asset_id", assetID.String())
			}
		} else {
			if err := database.SetAssetQuarantineEnabled(ctx, pool, assetID, true); err != nil && logger != nil {
				logger.Warn("compliance: quarantine_enabled update failed", "err", err)
			}
			downlink := map[string]string{
				"action":       "device_command",
				"command_id":   cmd.ID.String(),
				"command_type": models.DeviceCommandTypeQuarantine,
				"payload":      payload,
			}
			dispatched := false
			if dispatch != nil {
				dispatched = dispatch.DispatchJSON(certSerial, downlink)
			}
			if dispatched {
				if err := database.MarkDeviceCommandSent(ctx, pool, cmd.ID); err != nil && logger != nil {
					logger.Warn("compliance: mark auto quarantine sent failed", "err", err)
				}
			} else if logger != nil {
				logger.Info("compliance: auto quarantine queued pending agent connection",
					"asset_id", assetID.String(),
					"command_id", cmd.ID.String(),
				)
			}
		}
	}

	return nil
}

// QueueQuarantineCommand inserts a quarantine command and optionally dispatches it immediately.
func QueueQuarantineCommand(ctx context.Context, pool *pgxpool.Pool, dispatch C2Dispatcher,
	deviceID uuid.UUID, certSerial string, enabled bool,
) (models.DeviceCommand, bool, error) {
	var zero models.DeviceCommand
	if pool == nil {
		return zero, false, ErrNilPool
	}
	payload := BuildQuarantinePayloadJSON(enabled)
	cmd, err := database.InsertDeviceCommand(ctx, pool, deviceID, models.DeviceCommandTypeQuarantine, payload, nil)
	if err != nil {
		return zero, false, err
	}
	downlink := map[string]string{
		"action":       "device_command",
		"command_id":   cmd.ID.String(),
		"command_type": models.DeviceCommandTypeQuarantine,
		"payload":      payload,
	}
	if dispatch != nil && strings.TrimSpace(certSerial) != "" && dispatch.DispatchJSON(certSerial, downlink) {
		if err := database.MarkDeviceCommandSent(ctx, pool, cmd.ID); err != nil {
			return cmd, false, err
		}
		return cmd, true, nil
	}
	return cmd, false, nil
}
