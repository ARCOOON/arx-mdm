package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/mdm/policy"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EffectivePolicyDeps wires merge endpoints for dashboards and enrolled agents.
type EffectivePolicyDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	Auth         DashboardAuth
	MTLSRequired bool
}

type effectivePolicyHandler struct {
	deps EffectivePolicyDeps
}

// RegisterEffectivePolicyRoutes registers GET /v1/devices/{id}/effective-policy (dashboard JWT).
func RegisterEffectivePolicyRoutes(mux *http.ServeMux, d EffectivePolicyDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: RegisterEffectivePolicyRoutes requires Pool, Logger, Auth.JWT")
	}
	h := &effectivePolicyHandler{deps: d}
	mux.HandleFunc("GET /v1/devices/{id}/effective-policy", h.handleDashboardEffectivePolicy)
}

// RegisterAgentEffectivePolicyRoutes registers GET /v1/agent/effective-policy (client certificate identity).
func RegisterAgentEffectivePolicyRoutes(mux *http.ServeMux, d EffectivePolicyDeps) {
	if d.Pool == nil || d.Logger == nil {
		panic("api: RegisterAgentEffectivePolicyRoutes requires Pool and Logger")
	}
	h := &effectivePolicyHandler{deps: d}
	mux.HandleFunc("GET /v1/agent/effective-policy", h.handleAgentEffectivePolicy)
}

func assignedRowsToMergeInputs(rows []database.AssignedProfileRow) []policy.AssignedPayload {
	out := make([]policy.AssignedPayload, 0, len(rows))
	for _, r := range rows {
		src := policy.ProfileSource{ID: r.ID, Name: r.Name}
		raw := r.Payload
		if raw == nil {
			raw = json.RawMessage([]byte("{}"))
		}
		out = append(out, policy.AssignedPayload{Source: src, Payload: raw})
	}
	return out
}

func effectiveRevisionHex(payload map[string]any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func computeMergedEffectivePolicy(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, platformKey string) (*policy.MergeResult, error) {
	rows, err := database.ListAssignedProfilesForAsset(ctx, pool, assetID, platformKey)
	if err != nil {
		return nil, err
	}
	result, err := policy.MergeAssignedPayloads(assignedRowsToMergeInputs(rows))
	if err != nil {
		return nil, err
	}
	conflictIDs := make([]uuid.UUID, 0, len(result.ConflictProfiles))
	for id := range result.ConflictProfiles {
		conflictIDs = append(conflictIDs, id)
	}
	if err := database.ReconcileProfileAssignmentStates(ctx, pool, assetID, platformKey, conflictIDs); err != nil {
		return nil, err
	}
	return result, nil
}

func (h *effectivePolicyHandler) handleDashboardEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	raw := strings.TrimSpace(r.PathValue("id"))
	deviceID, err := uuid.Parse(raw)
	if err != nil || deviceID == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var exists int
	if err := h.deps.Pool.QueryRow(ctx, `SELECT 1 FROM assets WHERE id=$1::uuid LIMIT 1`, deviceID).Scan(&exists); err != nil {
		writeTicketsError(w, http.StatusNotFound, "device not found")
		return
	}

	var osType string
	if err := h.deps.Pool.QueryRow(ctx, `SELECT COALESCE(lower(trim(os_type)), 'unknown') FROM assets WHERE id=$1::uuid`, deviceID).Scan(&osType); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to resolve device platform")
		return
	}

	platformKey := telemetryPlatformKey(osType)
	mr, err := computeMergedEffectivePolicy(ctx, h.deps.Pool, deviceID, platformKey)
	if err != nil {
		h.deps.Logger.Error("effective policy merge failed", "err", err, "device_id", deviceID.String())
		writeTicketsError(w, http.StatusInternalServerError, "failed to compute effective policy")
		return
	}

	rev, err := effectiveRevisionHex(mr.EffectivePayload)
	if err != nil {
		h.deps.Logger.Error("effective policy revision failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to compute revision")
		return
	}

	resp := map[string]any{
		"asset_id":          deviceID.String(),
		"platform":          platformKey,
		"effective_payload": mr.EffectivePayload,
		"settings":          mr.FlatSettings,
		"conflicts":         mr.Conflicts,
		"has_conflict":      len(mr.Conflicts) > 0,
		"revision":          rev,
	}
	w.Header().Set("Cache-Control", "no-store")
	writeTicketsJSON(w, http.StatusOK, resp)
}

func (h *effectivePolicyHandler) handleAgentEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.deps.MTLSRequired || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		writeTicketsError(w, http.StatusForbidden, "mutual tls is required")
		return
	}
	leaf := r.TLS.PeerCertificates[0]
	serial := CertSerialDecimal(leaf)
	if serial == "" {
		writeTicketsError(w, http.StatusBadRequest, "client certificate serial invalid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var assetID uuid.UUID
	var osType string
	err := h.deps.Pool.QueryRow(ctx, `
SELECT id, COALESCE(lower(trim(os_type)), 'unknown')
FROM assets
WHERE cert_serial = $1
LIMIT 1
`, serial).Scan(&assetID, &osType)
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, "device not enrolled")
		return
	}

	platformKey := telemetryPlatformKey(osType)
	mr, err := computeMergedEffectivePolicy(ctx, h.deps.Pool, assetID, platformKey)
	if err != nil {
		h.deps.Logger.Error("agent effective policy merge failed", "err", err, "asset_id", assetID.String())
		writeTicketsError(w, http.StatusInternalServerError, "failed to compute effective policy")
		return
	}

	rev, err := effectiveRevisionHex(mr.EffectivePayload)
	if err != nil {
		h.deps.Logger.Error("agent effective policy revision failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to compute revision")
		return
	}

	resp := map[string]any{
		"asset_id":          assetID.String(),
		"os_type":           osType,
		"platform":          platformKey,
		"effective_payload": mr.EffectivePayload,
		"revision":          rev,
		"has_conflict":      len(mr.Conflicts) > 0,
	}
	w.Header().Set("Cache-Control", "no-store")
	writeTicketsJSON(w, http.StatusOK, resp)
}
