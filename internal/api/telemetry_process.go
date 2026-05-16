package api

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelemetryProcessResult is returned after a successful telemetry upsert.
type TelemetryProcessResult struct {
	AssetID  uuid.UUID
	HumanID  string
	Response map[string]any
}

// TelemetryProcessDeps configures shared telemetry persistence (HTTP and WebSocket).
type TelemetryProcessDeps struct {
	Pool            *pgxpool.Pool
	AdvisoryLockKey int64
	OnHeartbeat     func(ctx context.Context, assetID uuid.UUID)
	OnAccepted      func(certSerial, humanID string, assetID uuid.UUID, payload TelemetryPayload)
}

// ProcessTelemetry validates payload, upserts the asset row, and builds the agent response map.
func ProcessTelemetry(ctx context.Context, d TelemetryProcessDeps, certSerial string, payload TelemetryPayload) (TelemetryProcessResult, error) {
	var zero TelemetryProcessResult
	if d.Pool == nil {
		return zero, fmt.Errorf("telemetry: pool is required")
	}
	lockKey := d.AdvisoryLockKey
	if lockKey == 0 {
		lockKey = AdvisoryLockKeyARXClientSeq
	}

	payload.Hostname = strings.TrimSpace(payload.Hostname)
	if payload.Hostname == "" {
		return zero, fmt.Errorf("hostname is required")
	}
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return zero, fmt.Errorf("cert serial is required")
	}

	osType := deriveAssetOsType(payload)
	assetID, humanID, err := upsertAssetFromTelemetry(ctx, d.Pool, lockKey, certSerial, payload, osType)
	if err != nil {
		return zero, err
	}

	u64ToMetricInt64 := func(u uint64) int64 {
		if u > uint64(math.MaxInt64) {
			return math.MaxInt64
		}
		return int64(u)
	}
	if err := database.InsertDeviceMetric(ctx, d.Pool, assetID, payload.CPUUsagePercent,
		u64ToMetricInt64(payload.TotalRAMBytes),
		u64ToMetricInt64(payload.MemoryUsedBytes),
		u64ToMetricInt64(payload.RootDiskTotalBytes),
		u64ToMetricInt64(payload.RootDiskUsedBytes),
	); err != nil {
		return zero, fmt.Errorf("persist device metrics: %w", err)
	}

	if d.OnHeartbeat != nil {
		d.OnHeartbeat(ctx, assetID)
	}
	if d.OnAccepted != nil {
		d.OnAccepted(certSerial, humanID, assetID, payload)
	}

	resp := map[string]any{
		"status":   "ok",
		"asset_id": assetID.String(),
		"human_id": humanID,
	}
	if err := AppendAndroidPolicyToTelemetryResponse(ctx, d.Pool, assetID, osType, resp); err != nil {
		return zero, fmt.Errorf("android policy attachment: %w", err)
	}

	return TelemetryProcessResult{
		AssetID:  assetID,
		HumanID:  humanID,
		Response: resp,
	}, nil
}
