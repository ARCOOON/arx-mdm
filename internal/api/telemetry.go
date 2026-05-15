package api

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelemetryInstalledApp is one row of inventory included in telemetry JSON.
type TelemetryInstalledApp struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	ID      string `json:"id,omitempty"`
}

// TelemetryPayload is the JSON body sent by the agent for heartbeat/telemetry.
type TelemetryPayload struct {
	Hostname          string                  `json:"hostname"`
	OsType            string                  `json:"os_type,omitempty"`
	OSFamily          string                  `json:"os_family"`
	OSVersion         string                  `json:"os_version"`
	TotalRAMBytes     uint64                  `json:"total_ram_bytes"`
	CPUModel          string                  `json:"cpu_model"`
	CPULogicalCores   int                     `json:"cpu_logical_cores"`
	CPUUsagePercent   float64                 `json:"cpu_usage_percent,omitempty"`
	MemoryUsedBytes   uint64                  `json:"memory_used_bytes,omitempty"`
	BatteryPercent    float64                 `json:"battery_percent,omitempty"`
	DeviceModel       string                  `json:"device_model,omitempty"`
	MACAddress        string                  `json:"mac_address,omitempty"`
	InstalledSoftware []TelemetryInstalledApp `json:"installed_software"`
}

// TelemetryDeps carries dependencies for the telemetry HTTP handler.
type TelemetryDeps struct {
	Pool            *pgxpool.Pool
	Logger          *slog.Logger
	MTLSRequired    bool // false when server is not running with TLS + client CA configured
	AdvisoryLockKey int64
	// OnTelemetryAccepted is invoked after a successful upsert (optional). Used by the dashboard broadcaster.
	OnTelemetryAccepted func(certSerial, humanID string, assetID uuid.UUID, payload TelemetryPayload)
	// OnHeartbeat is invoked after a successful upsert (optional). Used to clear stale-heartbeat alert state.
	OnHeartbeat func(ctx context.Context, assetID uuid.UUID)
}

const (
	maxTelemetryBody = 256 << 10
	// AdvisoryLockKeyARXClientSeq namespaces PostgreSQL advisory locks for arx-c sequence allocation.
	AdvisoryLockKeyARXClientSeq int64 = 0x6172786300000001
)

// NewTelemetryHandler returns POST /v1/telemetry handler: mTLS-verified upsert of assets by cert serial or hostname.
func NewTelemetryHandler(d TelemetryDeps) http.HandlerFunc {
	if d.AdvisoryLockKey == 0 {
		d.AdvisoryLockKey = AdvisoryLockKeyARXClientSeq
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeTelemetryError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !d.MTLSRequired {
			writeTelemetryError(w, http.StatusServiceUnavailable, "telemetry requires ARX_TLS_CERT, ARX_TLS_KEY, and ARX_MTLS_CLIENT_CA_BUNDLE")
			return
		}
		if r.TLS == nil {
			writeTelemetryError(w, http.StatusForbidden, "tls is required for telemetry")
			return
		}
		if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
			writeTelemetryError(w, http.StatusForbidden, "mutual tls with a verified client certificate is required for telemetry")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxTelemetryBody))
		if cerr := r.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			d.Logger.Error("telemetry read body", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTelemetryError(w, http.StatusBadRequest, "could not read request body")
			return
		}

		var payload TelemetryPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			writeTelemetryError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		payload.Hostname = strings.TrimSpace(payload.Hostname)
		if payload.Hostname == "" {
			writeTelemetryError(w, http.StatusBadRequest, "hostname is required")
			return
		}

		leaf := r.TLS.PeerCertificates[0]
		serial := CertSerialDecimal(leaf)
		if serial == "" {
			writeTelemetryError(w, http.StatusBadRequest, "client certificate has no serial number")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := ProcessTelemetry(ctx, TelemetryProcessDeps{
			Pool:            d.Pool,
			AdvisoryLockKey: d.AdvisoryLockKey,
			OnHeartbeat:     d.OnHeartbeat,
			OnAccepted:      d.OnTelemetryAccepted,
		}, serial, payload)
		if err != nil {
			d.Logger.Error("telemetry upsert failed",
				"err", err,
				"request_id", r.Header.Get("X-Request-Id"),
				"cert_serial", serial,
				"hostname", payload.Hostname,
			)
			writeTelemetryError(w, http.StatusInternalServerError, "failed to persist telemetry")
			return
		}

		d.Logger.Info("telemetry accepted",
			"asset_id", result.AssetID.String(),
			"human_id", result.HumanID,
			"cert_serial", serial,
			"hostname", payload.Hostname,
			"os_family", payload.OSFamily,
			"os_type", deriveAssetOsType(payload),
			"request_id", r.Header.Get("X-Request-Id"),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result.Response)
	}
}

// CertSerialDecimal returns the leaf certificate serial number as a base-10 string, or empty if unavailable.
func CertSerialDecimal(cert *x509.Certificate) string {
	if cert == nil || cert.SerialNumber == nil {
		return ""
	}
	return cert.SerialNumber.Text(10)
}

// DeriveAssetOsType returns the normalized os_type classification for a telemetry payload (dashboard + DB).
func DeriveAssetOsType(p TelemetryPayload) string {
	return deriveAssetOsType(p)
}

func deriveAssetOsType(p TelemetryPayload) string {
	t := strings.ToLower(strings.TrimSpace(p.OsType))
	switch t {
	case "android", "windows", "linux", "darwin", "ios", "unknown":
		return t
	}
	f := strings.ToLower(strings.TrimSpace(p.OSFamily))
	switch f {
	case "android":
		return "android"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	case "darwin", "macos":
		return "darwin"
	case "ios", "iphone", "ipad":
		return "ios"
	default:
		return "unknown"
	}
}

func upsertAssetFromTelemetry(ctx context.Context, pool *pgxpool.Pool, lockKey int64, certSerial string, p TelemetryPayload, osType string) (uuid.UUID, string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
		return uuid.Nil, "", fmt.Errorf("advisory lock: %w", err)
	}

	metaPatch, err := json.Marshal(map[string]any{
		"telemetry": map[string]any{
			"os_family":           p.OSFamily,
			"os_version":          p.OSVersion,
			"os_type":             osType,
			"total_ram_bytes":     p.TotalRAMBytes,
			"cpu_model":           p.CPUModel,
			"cpu_logical_cores":   p.CPULogicalCores,
			"cpu_usage_percent":   p.CPUUsagePercent,
			"memory_used_bytes":   p.MemoryUsedBytes,
			"battery_percent":     p.BatteryPercent,
			"device_model":        p.DeviceModel,
			"mac_address":         p.MACAddress,
			"reported_hostname":   p.Hostname,
			"reported_at_rfc3339": time.Now().UTC().Format(time.RFC3339),
			"installed_software":  p.InstalledSoftware,
		},
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("marshal metadata patch: %w", err)
	}

	var id uuid.UUID
	var humanID string
	found := false

	err = tx.QueryRow(ctx, `
SELECT id, human_id
FROM assets
WHERE cert_serial = $1
LIMIT 1
FOR UPDATE
`, certSerial).Scan(&id, &humanID)
	if err == nil {
		found = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", fmt.Errorf("lookup by cert_serial: %w", err)
	}

	if !found {
		err = tx.QueryRow(ctx, `
SELECT id, human_id
FROM assets
WHERE hostname = $1
LIMIT 1
FOR UPDATE
`, p.Hostname).Scan(&id, &humanID)
		if err == nil {
			found = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", fmt.Errorf("lookup by hostname: %w", err)
		}
	}

	if !found {
		var next int64
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(MAX(
  CASE WHEN human_id ~ '^arx-c-[0-9]+$'
       THEN SUBSTRING(human_id FROM 7)::bigint
  END
), 0) + 1
FROM assets
`).Scan(&next); err != nil {
			return uuid.Nil, "", fmt.Errorf("next arx-c sequence: %w", err)
		}
		humanID = fmt.Sprintf("arx-c-%d", next)
		display := p.Hostname
		if err := tx.QueryRow(ctx, `
INSERT INTO assets (human_id, display_name, hostname, cert_serial, os_type, last_seen, metadata, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), $6::jsonb, now())
RETURNING id
`, humanID, display, p.Hostname, certSerial, osType, metaPatch).Scan(&id); err != nil {
			return uuid.Nil, "", fmt.Errorf("insert asset: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, "", fmt.Errorf("commit insert: %w", err)
		}
		return id, humanID, nil
	}

	_, err = tx.Exec(ctx, `
UPDATE assets
SET last_seen = now(),
    updated_at = now(),
    hostname = $1,
    cert_serial = $2,
    os_type = $3,
    display_name = CASE WHEN display_name IS NULL OR display_name = '' THEN $1 ELSE display_name END,
    metadata = COALESCE(metadata, '{}'::jsonb) || $4::jsonb
WHERE id = $5
`, p.Hostname, certSerial, osType, metaPatch, id)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("update asset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, "", fmt.Errorf("commit update: %w", err)
	}
	return id, humanID, nil
}

type telemetryErrorJSON struct {
	Error string `json:"error"`
}

func writeTelemetryError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(telemetryErrorJSON{Error: msg})
}
