package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"arx-mdm/internal/auth"
	"arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAndroidPolicyJSONBody = 16 << 10

// AndroidPolicyWire is the JSON shape for dashboard REST, WebSocket pushes, and telemetry downlink.
type AndroidPolicyWire struct {
	CameraDisabled      bool  `json:"camera_disabled"`
	ScreenLockTimeoutMs int64 `json:"screen_lock_timeout_ms"`
	WipeRequested       bool  `json:"wipe_requested"`
}

// DashboardFanout broadcasts JSON to connected dashboard WebSocket clients (e.g. *ws.DashboardHub).
type DashboardFanout interface {
	Broadcast(data []byte)
}

// AndroidPoliciesDeps wires Android policy REST and optional dashboard fan-out.
type AndroidPoliciesDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Auth    DashboardAuth
	DashHub DashboardFanout
	// OnAndroidRemoteWipeRequested is optional; invoked when an operator requests remote wipe (false → true).
	OnAndroidRemoteWipeRequested func(ctx context.Context, assetID uuid.UUID, humanID string)
}

// RegisterAndroidPolicyRoutes registers GET/PATCH /v1/assets/{id}/android-policy.
func RegisterAndroidPolicyRoutes(mux *http.ServeMux, d AndroidPoliciesDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: android policy routes require Pool, Logger, and Auth.JWT")
	}
	h := &androidPoliciesHandler{deps: d}
	mux.HandleFunc("GET /v1/assets/{id}/android-policy", h.handleGet)
	mux.HandleFunc("PATCH /v1/assets/{id}/android-policy", h.handlePatch)
}

type androidPoliciesHandler struct {
	deps AndroidPoliciesDeps
}

type patchAndroidPolicyRequest struct {
	CameraDisabled      *bool  `json:"camera_disabled"`
	ScreenLockTimeoutMs *int64 `json:"screen_lock_timeout_ms"`
	WipeRequested       *bool  `json:"wipe_requested"`
}

func parseAssetUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, errors.New("missing id")
	}
	return uuid.Parse(raw)
}

func (h *androidPoliciesHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	assetID, err := parseAssetUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid asset id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if !assetExists(ctx, h.deps.Pool, assetID) {
		writeTicketsError(w, http.StatusNotFound, "asset not found")
		return
	}
	pol, err := loadAndroidPolicy(ctx, h.deps.Pool, assetID)
	if err != nil {
		h.deps.Logger.Error("load android policy", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load policy")
		return
	}
	writeTicketsJSON(w, http.StatusOK, pol)
}

func (h *androidPoliciesHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	assetID, err := parseAssetUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid asset id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAndroidPolicyJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read body")
		return
	}
	var req patchAndroidPolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	humanID, err := humanIDForAsset(ctx, h.deps.Pool, assetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "asset not found")
			return
		}
		h.deps.Logger.Error("resolve asset", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
		return
	}
	prev, err := loadAndroidPolicy(ctx, h.deps.Pool, assetID)
	if err != nil {
		h.deps.Logger.Error("load android policy before patch", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to save policy")
		return
	}
	out, err := mergeAndroidPolicy(ctx, h.deps.Pool, assetID, req)
	if err != nil {
		h.deps.Logger.Error("merge android policy", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to save policy")
		return
	}
	wipeRequested := out.WipeRequested && !prev.WipeRequested && req.WipeRequested != nil && *req.WipeRequested
	if wipeRequested && h.deps.OnAndroidRemoteWipeRequested != nil {
		evCtx, evCancel := context.WithTimeout(context.Background(), 5*time.Second)
		go func() {
			defer evCancel()
			defer func() {
				if rec := recover(); rec != nil {
					h.deps.Logger.Error("OnAndroidRemoteWipeRequested panicked", "recover", rec)
				}
			}()
			h.deps.OnAndroidRemoteWipeRequested(evCtx, assetID, humanID)
		}()
	}
	h.broadcastUpdated(assetID, humanID, out)
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *androidPoliciesHandler) broadcastUpdated(assetID uuid.UUID, humanID string, pol AndroidPolicyWire) {
	if h.deps.DashHub == nil {
		return
	}
	b, err := json.Marshal(map[string]any{
		"type":     "android_policy_updated",
		"asset_id": assetID.String(),
		"human_id": humanID,
		"policy":   pol,
	})
	if err != nil {
		return
	}
	h.deps.DashHub.Broadcast(b)
}

func assetExists(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) bool {
	var one int
	_ = pool.QueryRow(ctx, `SELECT 1 FROM assets WHERE id = $1 LIMIT 1`, assetID).Scan(&one)
	return one == 1
}

func humanIDForAsset(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) (string, error) {
	var hid string
	err := pool.QueryRow(ctx, `SELECT human_id FROM assets WHERE id = $1 LIMIT 1`, assetID).Scan(&hid)
	return hid, err
}

func loadAndroidPolicy(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) (AndroidPolicyWire, error) {
	var p AndroidPolicyWire
	err := pool.QueryRow(ctx, `
SELECT camera_disabled, screen_lock_timeout_ms, wipe_requested
FROM android_policies
WHERE asset_id = $1
`, assetID).Scan(&p.CameraDisabled, &p.ScreenLockTimeoutMs, &p.WipeRequested)
	if errors.Is(err, pgx.ErrNoRows) {
		return AndroidPolicyWire{}, nil
	}
	if err != nil {
		return AndroidPolicyWire{}, err
	}
	return p, nil
}

func mergeAndroidPolicy(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, req patchAndroidPolicyRequest) (AndroidPolicyWire, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return AndroidPolicyWire{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var cur models.AndroidPolicy
	err = tx.QueryRow(ctx, `
SELECT asset_id, camera_disabled, screen_lock_timeout_ms, wipe_requested, updated_at
FROM android_policies
WHERE asset_id = $1
FOR UPDATE
`, assetID).Scan(&cur.AssetID, &cur.CameraDisabled, &cur.ScreenLockTimeoutMs, &cur.WipeRequested, &cur.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		cur = models.AndroidPolicy{
			AssetID:             assetID,
			CameraDisabled:      false,
			ScreenLockTimeoutMs: 0,
			WipeRequested:       false,
		}
	} else if err != nil {
		return AndroidPolicyWire{}, err
	}

	if req.CameraDisabled != nil {
		cur.CameraDisabled = *req.CameraDisabled
	}
	if req.ScreenLockTimeoutMs != nil {
		cur.ScreenLockTimeoutMs = *req.ScreenLockTimeoutMs
		if cur.ScreenLockTimeoutMs < 0 {
			cur.ScreenLockTimeoutMs = 0
		}
	}
	if req.WipeRequested != nil {
		cur.WipeRequested = *req.WipeRequested
	}

	_, err = tx.Exec(ctx, `
INSERT INTO android_policies (asset_id, camera_disabled, screen_lock_timeout_ms, wipe_requested, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (asset_id) DO UPDATE SET
    camera_disabled = EXCLUDED.camera_disabled,
    screen_lock_timeout_ms = EXCLUDED.screen_lock_timeout_ms,
    wipe_requested = EXCLUDED.wipe_requested,
    updated_at = now()
`, assetID, cur.CameraDisabled, cur.ScreenLockTimeoutMs, cur.WipeRequested)
	if err != nil {
		return AndroidPolicyWire{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AndroidPolicyWire{}, err
	}
	return AndroidPolicyWire{
		CameraDisabled:      cur.CameraDisabled,
		ScreenLockTimeoutMs: cur.ScreenLockTimeoutMs,
		WipeRequested:       cur.WipeRequested,
	}, nil
}

// AppendAndroidPolicyToTelemetryResponse adds android_policy to resp when the asset is Android-classified.
// If wipe was active, it is cleared after the wire snapshot so the next heartbeat does not loop wipe.
func AppendAndroidPolicyToTelemetryResponse(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, osType string, resp map[string]any) error {
	if !strings.EqualFold(strings.TrimSpace(osType), "android") {
		return nil
	}
	wire, err := loadAndroidPolicy(ctx, pool, assetID)
	if err != nil {
		return err
	}
	hadWipe := wire.WipeRequested
	resp["android_policy"] = wire
	if hadWipe {
		_, _ = pool.Exec(ctx, `
UPDATE android_policies
SET wipe_requested = false, updated_at = now()
WHERE asset_id = $1 AND wipe_requested = true
`, assetID)
	}
	return nil
}
