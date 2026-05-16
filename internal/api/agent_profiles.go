package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentProfilesDeps serves GET /v1/agent/configuration-profiles for enrolled endpoints over mTLS.
type AgentProfilesDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	MTLSRequired bool
}

type agentProfilesHandler struct {
	deps AgentProfilesDeps
}

// RegisterAgentProfilesRoutes registers declarative synchronization for managed agents only.
func RegisterAgentProfilesRoutes(mux *http.ServeMux, d AgentProfilesDeps) {
	if d.Pool == nil || d.Logger == nil {
		panic("api: RegisterAgentProfilesRoutes requires Pool and Logger")
	}
	h := &agentProfilesHandler{deps: d}
	mux.HandleFunc("GET /v1/agent/configuration-profiles", h.handlePull)
}

func (h *agentProfilesHandler) handlePull(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
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

	profilesWire, managed, err := database.BuildMDMProfilesWireForAsset(ctx, h.deps.Pool, assetID, telemetryPlatformKey(osType))
	if err != nil {
		h.deps.Logger.Error("agent profile bundle failed", "err", err, "asset_id", assetID.String())
		writeTicketsError(w, http.StatusInternalServerError, "failed to assemble profiles")
		return
	}

	revPayload, err := json.Marshal(profilesWire)
	if err != nil {
		h.deps.Logger.Error("marshal profile revision", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to marshal profiles")
		return
	}
	sum := sha256.Sum256(revPayload)

	out := map[string]any{
		"revision": hex.EncodeToString(sum[:]),
		"os_type":  osType,
		"asset_id": assetID.String(),
		"profiles": profilesWire,
	}
	if len(profilesWire) == 0 {
		out["profiles"] = []any{}
	}
	if len(managed) > 0 {
		out["managed_app_configs"] = managed
	}

	w.Header().Set("Cache-Control", "no-store")
	writeTicketsJSON(w, http.StatusOK, out)
}
