package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxAppCatalogJSON = 64 << 10
	maxAppUploadBytes = 512 << 20
)

// AppCatalogDeps exposes dashboard catalog management and deployments.
type AppCatalogDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
	C2Hub  C2Hub
	AppsRoot string
}

type appCatalogHandler struct {
	deps AppCatalogDeps
}

// RegisterAppCatalogRoutes registers app catalog CRUD, upload, and per-device deployment endpoints.
func RegisterAppCatalogRoutes(mux *http.ServeMux, d AppCatalogDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: app catalog requires Pool, Logger, and Auth.JWT")
	}
	root := strings.TrimSpace(d.AppsRoot)
	if root == "" {
		panic("api: app catalog requires AppsRoot")
	}
	h := &appCatalogHandler{deps: d}
	mux.HandleFunc("GET /v1/app-catalog", h.handleListCatalog)
	mux.HandleFunc("POST /v1/app-catalog", h.handleCreateCatalogJSON)
	mux.HandleFunc("POST /v1/app-catalog/upload", h.handleUploadCatalog)
	mux.HandleFunc("GET /v1/app-catalog/{id}", h.handleGetCatalog)
	mux.HandleFunc("PATCH /v1/app-catalog/{id}", h.handlePatchCatalog)
	mux.HandleFunc("DELETE /v1/app-catalog/{id}", h.handleDeleteCatalog)
	mux.HandleFunc("GET /v1/devices/{id}/app-deployments", h.handleListDeviceApps)
	mux.HandleFunc("POST /v1/devices/{id}/app-deployments", h.handleAssignApp)
}

func (h *appCatalogHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *appCatalogHandler) authorizeOperator(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	return ok
}

func parseAppCatalogUUIDParam(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
	}
	return uuid.Parse(raw)
}

type appCatalogWire struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	TargetOS      string    `json:"target_os"`
	FilePathOrURL string    `json:"file_path_or_url"`
	InstallArgs   string    `json:"install_args"`
	CreatedAt     time.Time `json:"created_at"`
}

type createCatalogJSONRequest struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	TargetOS      string `json:"target_os"`
	FilePathOrURL string `json:"file_path_or_url"`
	InstallArgs   string `json:"install_args"`
}

type patchCatalogRequest struct {
	Name          *string `json:"name"`
	Version       *string `json:"version"`
	TargetOS      *string `json:"target_os"`
	FilePathOrURL *string `json:"file_path_or_url"`
	InstallArgs   *string `json:"install_args"`
}

type assignAppRequest struct {
	AppID string `json:"app_id"`
}

type deviceAppWire struct {
	DeviceID      uuid.UUID `json:"device_id"`
	AppID         uuid.UUID `json:"app_id"`
	AppName       string    `json:"app_name"`
	AppVersion    string    `json:"app_version"`
	TargetOS      string    `json:"target_os"`
	Status        string    `json:"status"`
	ErrorMessage  *string   `json:"error_message,omitempty"`
	LastUpdated   time.Time `json:"last_updated"`
}

func normalizeTargetOS(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "windows":
		return "windows", nil
	case "linux":
		return "linux", nil
	case "android":
		return "android", nil
	default:
		return "", fmt.Errorf("invalid target_os")
	}
}

func sanitizeUploadBaseName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return "package.bin"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r), r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" || out == "." {
		return "package.bin"
	}
	return out
}

func (h *appCatalogHandler) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, name, COALESCE(version, ''), target_os,
       COALESCE(file_path_or_url, ''), COALESCE(install_args, ''), created_at
FROM app_catalog
ORDER BY created_at DESC`)
	if err != nil {
		h.deps.Logger.Error("list app catalog", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list catalog")
		return
	}
	defer rows.Close()
	var out []appCatalogWire
	for rows.Next() {
		var row appCatalogWire
		if scanErr := rows.Scan(&row.ID, &row.Name, &row.Version, &row.TargetOS,
			&row.FilePathOrURL, &row.InstallArgs, &row.CreatedAt); scanErr != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to list catalog")
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list catalog")
		return
	}
	if out == nil {
		out = []appCatalogWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *appCatalogHandler) handleCreateCatalogJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAppCatalogJSON))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createCatalogJSONRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Version = strings.TrimSpace(req.Version)
	req.FilePathOrURL = strings.TrimSpace(req.FilePathOrURL)
	req.InstallArgs = strings.TrimSpace(req.InstallArgs)
	if req.Name == "" {
		writeTicketsError(w, http.StatusBadRequest, "name is required")
		return
	}
	tos, err := normalizeTargetOS(req.TargetOS)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.FilePathOrURL == "" {
		writeTicketsError(w, http.StatusBadRequest, "file_path_or_url is required")
		return
	}
	if !looksLikeHTTPSURL(req.FilePathOrURL) {
		writeTicketsError(w, http.StatusBadRequest, "file_path_or_url must be https:// URL for metadata-only catalog entries")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO app_catalog (name, version, target_os, file_path_or_url, install_args)
VALUES ($1,$2,$3,$4,$5)
RETURNING id
`, req.Name, req.Version, tos, req.FilePathOrURL, req.InstallArgs).Scan(&id)
	if err != nil {
		h.deps.Logger.Error("insert catalog", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create catalog row")
		return
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *appCatalogHandler) handleUploadCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	if err := r.ParseMultipartForm(int64(maxAppUploadBytes)); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	version := strings.TrimSpace(r.FormValue("version"))
	tos, err := normalizeTargetOS(r.FormValue("target_os"))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}
	installArgs := strings.TrimSpace(r.FormValue("install_args"))
	if name == "" {
		writeTicketsError(w, http.StatusBadRequest, "name is required")
		return
	}
	fh, fileHeader, err := r.FormFile("file")
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer fh.Close()
	if fileHeader.Size > maxAppUploadBytes {
		writeTicketsError(w, http.StatusBadRequest, "file too large")
		return
	}

	relName := fmt.Sprintf("blobs/%s_%s", uuid.NewString(), sanitizeUploadBaseName(fileHeader.Filename))
	absPath, err := safeAppsPath(h.deps.AppsRoot, relName)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "storage path error")
		return
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		h.deps.Logger.Error("mkdir apps root", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to persist upload")
		return
	}

	tmp := absPath + ".partial"
	dest, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to persist upload")
		return
	}
	n, cpErr := io.Copy(dest, io.LimitReader(fh, int64(maxAppUploadBytes)))
	_ = dest.Close()
	if cpErr != nil {
		_ = os.Remove(tmp)
		writeTicketsError(w, http.StatusBadRequest, "failed to stream upload body")
		return
	}
	if n == int64(maxAppUploadBytes) {
		_ = os.Remove(tmp)
		writeTicketsError(w, http.StatusBadRequest, "file too large")
		return
	}
	if err := os.Rename(tmp, absPath); err != nil {
		_ = os.Remove(tmp)
		writeTicketsError(w, http.StatusInternalServerError, "finalize upload failed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var id uuid.UUID
	insertErr := h.deps.Pool.QueryRow(ctx, `
INSERT INTO app_catalog (name, version, target_os, file_path_or_url, install_args)
VALUES ($1,$2,$3,$4,$5)
RETURNING id
`, name, version, tos, relName, installArgs).Scan(&id)
	if insertErr != nil {
		h.deps.Logger.Error("catalog insert after upload", "err", insertErr)
		writeTicketsError(w, http.StatusInternalServerError, "failed to persist catalog row")
		return
	}

	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *appCatalogHandler) handleGetCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	id, err := parseAppCatalogUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var row appCatalogWire
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), target_os,
       COALESCE(file_path_or_url, ''), COALESCE(install_args, ''), created_at
FROM app_catalog WHERE id = $1
`, id).Scan(&row.ID, &row.Name, &row.Version, &row.TargetOS, &row.FilePathOrURL, &row.InstallArgs, &row.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "catalog entry not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeTicketsJSON(w, http.StatusOK, row)
}

func (h *appCatalogHandler) handlePatchCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseAppCatalogUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAppCatalogJSON))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchCatalogRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var cur models.AppCatalog
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), target_os, COALESCE(file_path_or_url, ''), COALESCE(install_args, '')
FROM app_catalog WHERE id = $1
`, id).Scan(&cur.ID, &cur.Name, &cur.Version, &cur.TargetOS, &cur.FilePathOrURL, &cur.InstallArgs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "catalog entry not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	name := cur.Name
	ver := cur.Version
	tos := cur.TargetOS
	loc := cur.FilePathOrURL
	args := cur.InstallArgs
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if req.Version != nil {
		ver = strings.TrimSpace(*req.Version)
	}
	if req.TargetOS != nil {
		tosNorm, normErr := normalizeTargetOS(*req.TargetOS)
		if normErr != nil {
			writeTicketsError(w, http.StatusBadRequest, normErr.Error())
			return
		}
		tos = tosNorm
	}
	if req.FilePathOrURL != nil {
		loc = strings.TrimSpace(*req.FilePathOrURL)
		if loc != "" && !looksLikeHTTPSURL(loc) && !looksLikeUploadedBlobPath(loc) {
			writeTicketsError(w, http.StatusBadRequest, "file_path_or_url must remain a staged blobs/ path or an https:// URL")
			return
		}
	}
	if req.InstallArgs != nil {
		args = strings.TrimSpace(*req.InstallArgs)
	}
	if name == "" {
		writeTicketsError(w, http.StatusBadRequest, "name is required")
		return
	}
	if loc == "" {
		writeTicketsError(w, http.StatusBadRequest, "file_path_or_url is required")
		return
	}

	_, err = h.deps.Pool.Exec(ctx, `
UPDATE app_catalog
SET name = $2, version = $3, target_os = $4, file_path_or_url = $5, install_args = $6
WHERE id = $1
`, id, name, ver, tos, loc, args)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func looksLikeUploadedBlobPath(s string) bool {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	return strings.HasPrefix(s, "blobs/")
}

func (h *appCatalogHandler) handleDeleteCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseAppCatalogUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var loc string
	err = h.deps.Pool.QueryRow(ctx, `SELECT COALESCE(file_path_or_url, '') FROM app_catalog WHERE id = $1`, id).Scan(&loc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "catalog entry not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM app_catalog WHERE id = $1`, id)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "catalog entry not found")
		return
	}

	if looksLikeUploadedBlobPath(loc) {
		if absPath, pathErr := safeAppsPath(h.deps.AppsRoot, loc); pathErr == nil {
			if rmErr := os.Remove(absPath); rmErr != nil && h.deps.Logger != nil && !errors.Is(rmErr, os.ErrNotExist) {
				h.deps.Logger.Debug("unlink blob after catalog delete", "err", rmErr)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func assetOsMatchesTarget(deviceOS, target string) bool {
	deviceOS = strings.ToLower(strings.TrimSpace(deviceOS))
	target = strings.ToLower(strings.TrimSpace(target))
	switch target {
	case "windows":
		return deviceOS == "windows"
	case "linux":
		return deviceOS == "linux"
	case "android":
		return deviceOS == "android"
	default:
		return false
	}
}

func (h *appCatalogHandler) handleListDeviceApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	deviceID, ok := parseDeviceAssignmentUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var exists bool
	if err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id = $1)`, deviceID).Scan(&exists); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !exists {
		writeTicketsError(w, http.StatusNotFound, "device not found")
		return
	}
	rows, err := h.deps.Pool.Query(ctx, `
SELECT da.device_id, da.app_id, ac.name, COALESCE(ac.version, ''), ac.target_os,
       da.status, da.error_message, da.last_updated
FROM device_apps da
JOIN app_catalog ac ON ac.id = da.app_id
WHERE da.device_id = $1
ORDER BY da.last_updated DESC`, deviceID)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	var out []deviceAppWire
	for rows.Next() {
		var row deviceAppWire
		if scanErr := rows.Scan(&row.DeviceID, &row.AppID, &row.AppName, &row.AppVersion, &row.TargetOS,
			&row.Status, &row.ErrorMessage, &row.LastUpdated); scanErr != nil {
			writeTicketsError(w, http.StatusInternalServerError, "list failed")
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if out == nil {
		out = []deviceAppWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *appCatalogHandler) handleAssignApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	deviceID, ok := parseDeviceAssignmentUUID(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAppCatalogJSON))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req assignAppRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	appID, err := uuid.Parse(strings.TrimSpace(req.AppID))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid app_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var humanID string
	var certSerial *string
	var osType string
	err = h.deps.Pool.QueryRow(ctx, `SELECT human_id, cert_serial, COALESCE(os_type, '') FROM assets WHERE id = $1`, deviceID).Scan(&humanID, &certSerial, &osType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "device not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if strings.TrimSpace(humanID) == "" {
		writeTicketsError(w, http.StatusBadRequest, "device has no human_id")
		return
	}
	if certSerial == nil || strings.TrimSpace(*certSerial) == "" {
		writeTicketsError(w, http.StatusBadRequest, "device is not enrolled with a certificate serial")
		return
	}

	var row models.AppCatalog
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), target_os, COALESCE(file_path_or_url, ''), COALESCE(install_args, '')
FROM app_catalog WHERE id = $1
`, appID).Scan(&row.ID, &row.Name, &row.Version, &row.TargetOS, &row.FilePathOrURL, &row.InstallArgs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "catalog entry not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !assetOsMatchesTarget(osType, row.TargetOS) {
		writeTicketsError(w, http.StatusBadRequest, "device os_type does not match catalog target_os")
		return
	}

	_, err = h.deps.Pool.Exec(ctx, `
INSERT INTO device_apps (device_id, app_id, status, last_updated, error_message)
VALUES ($1, $2, 'pending', now(), NULL)
ON CONFLICT (device_id, app_id) DO UPDATE
SET status = 'pending', last_updated = now(), error_message = NULL
`, deviceID, appID)
	if err != nil {
		h.deps.Logger.Error("upsert device_apps", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to schedule deployment")
		return
	}

	artifactPath := strings.TrimSpace(row.FilePathOrURL)
	if looksLikeHTTPSURL(artifactPath) {
		// Full URL for agents that can reach the hosting origin (Android / Go use HTTPS GET with mTLS client only toward MDM origin;
		// external URLs rely on HTTPS without client auth).
	} else if looksLikeUploadedBlobPath(artifactPath) {
		artifactPath = "/v1/agent/app-artifacts/" + appID.String()
	} else {
		writeTicketsError(w, http.StatusBadRequest, "catalog entry missing staged blob or HTTPS URL")
		return
	}

	payload := map[string]any{
		"action":         "install_app",
		"app_id":         appID.String(),
		"artifact_path":  artifactPath,
		"install_args":   row.InstallArgs,
		"target_os_hint": strings.ToLower(strings.TrimSpace(row.TargetOS)),
	}

	dispatchErrMsg := ""
	if h.deps.C2Hub == nil {
		dispatchErrMsg = "c2 hub not configured"
	} else {
		dispatchErr := h.deps.C2Hub.DispatchJSONByHumanID(ctx, h.deps.Pool, humanID, payload)
		if dispatchErr != nil {
			dispatchErrMsg = dispatchErr.Error()
		}
	}

	if dispatchErrMsg != "" {
		em := dispatchErrMsg
		_, _ = h.deps.Pool.Exec(ctx, `
UPDATE device_apps SET status = 'failed', error_message = $3, last_updated = now()
WHERE device_id = $1 AND app_id = $2
`, deviceID, appID, em)
		writeTicketsJSON(w, http.StatusOK, map[string]any{
			"status":            "queued",
			"dispatch_error":    em,
			"dispatch_succeeded": false,
		})
		return
	}

	_, _ = h.deps.Pool.Exec(ctx, `
UPDATE device_apps SET status = 'installing', last_updated = now()
WHERE device_id = $1 AND app_id = $2
`, deviceID, appID)

	writeTicketsJSON(w, http.StatusCreated, map[string]any{
		"status":             "dispatched",
		"dispatch_succeeded": true,
		"dispatch_error":     nil,
	})
}
