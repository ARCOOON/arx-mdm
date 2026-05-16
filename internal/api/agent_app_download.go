package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errInvalidStoredPath = errors.New("storage path rejected")

// AgentAppArtifactDeps serves GET /v1/agent/app-artifacts/{id} for enrolled agents using mutual TLS only.
type AgentAppArtifactDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	MTLSRequired bool
	AppsRoot     string // ARX_APPS_STORAGE_PATH absolute or relative path on server host
}

// RegisterAgentAppArtifactRoutes registers staged catalog binary downloads authenticated by enrollment cert serial + device_apps mapping.
func RegisterAgentAppArtifactRoutes(mux *http.ServeMux, d AgentAppArtifactDeps) {
	if d.Pool == nil || d.Logger == nil || strings.TrimSpace(d.AppsRoot) == "" {
		panic("api: RegisterAgentAppArtifactRoutes requires Pool, Logger, AppsRoot")
	}
	h := &agentAppArtifactHandler{deps: d}
	mux.HandleFunc("GET /v1/agent/app-artifacts/{id}", h.handleDownload)
}

type agentAppArtifactHandler struct {
	deps AgentAppArtifactDeps
}

func (h *agentAppArtifactHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTelemetryError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.deps.MTLSRequired || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		writeTelemetryError(w, http.StatusForbidden, "mutual tls is required")
		return
	}
	rawID := strings.TrimSpace(r.PathValue("id"))
	appID, err := uuid.Parse(rawID)
	if err != nil {
		writeTelemetryError(w, http.StatusBadRequest, "invalid app id")
		return
	}
	leaf := r.TLS.PeerCertificates[0]
	serial := CertSerialDecimal(leaf)
	if serial == "" {
		writeTelemetryError(w, http.StatusBadRequest, "client certificate serial invalid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	var row struct {
		RelPath string
		Name    string
	}
	q := `
SELECT COALESCE(ac.file_path_or_url, ''),
       COALESCE(ac.name, 'artifact')
FROM app_catalog ac
JOIN device_apps da ON da.app_id = ac.id
JOIN assets a ON a.id = da.device_id AND a.cert_serial = $2
WHERE ac.id = $1
  AND da.status IN ('pending', 'installing')
`
	err = h.deps.Pool.QueryRow(ctx, q, appID, serial).Scan(&row.RelPath, &row.Name)
	if err != nil {
		writeTelemetryError(w, http.StatusNotFound, "artifact not authorized or unavailable")
		return
	}
	if looksLikeHTTPSURL(row.RelPath) {
		writeTelemetryError(w, http.StatusNotFound, "this catalog entry references an external url; configure direct download metadata on the endpoint")
		return
	}

	abs, err := safeAppsPath(h.deps.AppsRoot, row.RelPath)
	if err != nil {
		if h.deps.Logger != nil {
			h.deps.Logger.Warn("unsafe apps path", "err", err)
		}
		writeTelemetryError(w, http.StatusInternalServerError, "storage path rejected")
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		writeTelemetryError(w, http.StatusNotFound, "file not found")
		return
	}
	defer f.Close()
	st, statErr := f.Stat()
	if statErr != nil || st.IsDir() {
		writeTelemetryError(w, http.StatusNotFound, "artifact missing")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil && h.deps.Logger != nil {
		h.deps.Logger.Debug("artifact stream ended", "err", err)
	}
}

// looksLikeHTTPSURL reports whether the configured catalog locator is an explicit HTTP(S) URI.
func looksLikeHTTPSURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

func safeAppsPath(rootDir, stored string) (string, error) {
	stored = strings.TrimSpace(strings.ReplaceAll(stored, "\\", "/"))
	stored = strings.TrimPrefix(stored, "/")
	if stored == "" || strings.Contains(stored, "..") {
		return "", errInvalidStoredPath
	}
	absRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(rootDir)))
	if err != nil {
		return "", err
	}
	target := filepath.Clean(filepath.Join(absRoot, filepath.FromSlash(stored)))
	rel, err := filepath.Rel(absRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errInvalidStoredPath
	}
	return target, nil
}
