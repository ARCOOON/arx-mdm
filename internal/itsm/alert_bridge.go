package itsm

import (
	"context"
	"log/slog"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IncidentAlertBridge wires alerting fingerprints into persisted incidents without import cycles from alerting/.
type IncidentAlertBridge struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
}

// OnCriticalDeviceFingerprint opens or bumps an automation incident keyed by fingerprint.
func (b *IncidentAlertBridge) OnCriticalDeviceFingerprint(ctx context.Context, fingerprint, severity string, deviceID uuid.UUID, title, message string) {
	if b == nil || b.Pool == nil {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	if err := database.UpsertIncidentForCriticalFingerprint(runCtx, b.Pool, fingerprint, deviceID, title, message, severity); err != nil && b.Log != nil {
		b.Log.Warn("incident automation: upsert incident failed", "err", err, "fingerprint", fingerprint)
	}
}

// OnAlertFingerprintsResolved notifies ITSM workflows that monitoring cleared active alerts.
func (b *IncidentAlertBridge) OnAlertFingerprintsResolved(ctx context.Context, fingerprints []string) {
	if b == nil || b.Pool == nil || len(fingerprints) == 0 {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	if err := database.AutoResolveIncidentsForAlertFingerprints(runCtx, b.Pool, fingerprints); err != nil && b.Log != nil {
		b.Log.Warn("incident automation: auto-resolve failed", "err", err)
	}
}
