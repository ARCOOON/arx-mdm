package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const maxPackageJSONBody = 256 << 10

// C2Hub delivers JSON commands to a connected agent session (implemented by *ws.Hub).
type C2Hub interface {
	DispatchJSONByHumanID(ctx context.Context, pool *pgxpool.Pool, humanID string, payload any) error
}

// PackagesDeps wires the packages and deployments REST API.
type PackagesDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Auth    DashboardAuth
	C2Hub   C2Hub
}

var allowedPackageTypes = map[string]struct{}{
	"winget": {}, "apt": {}, "dnf": {}, "choco": {}, "custom": {},
}

type packagesHandler struct {
	deps PackagesDeps
}

// NewPackagesHandler registers dashboard-authenticated package and deployment routes.
func NewPackagesHandler(mux *http.ServeMux, d PackagesDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: packages handler requires Pool, Logger, and Auth.JWT")
	}
	h := &packagesHandler{deps: d}
	mux.HandleFunc("GET /v1/packages", h.handleListPackages)
	mux.HandleFunc("POST /v1/packages", h.handleCreatePackage)
	mux.HandleFunc("GET /v1/packages/{id}", h.handleGetPackage)
	mux.HandleFunc("PATCH /v1/packages/{id}", h.handlePatchPackage)
	mux.HandleFunc("DELETE /v1/packages/{id}", h.handleDeletePackage)
	mux.HandleFunc("GET /v1/deployments", h.handleListDeployments)
	mux.HandleFunc("POST /v1/deployments", h.handleCreateDeployment)
}

func (h *packagesHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *packagesHandler) authorizeOperator(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	return ok
}

func (h *packagesHandler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin)
	return ok
}

func parsePackageUUIDParam(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
	}
	return uuid.Parse(raw)
}

// --- packages ---

type packageWire struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Type        string    `json:"type"`
	InstallCmd  string    `json:"install_cmd"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type createPackageRequest struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Type       string `json:"type"`
	InstallCmd string `json:"install_cmd"`
}

type patchPackageRequest struct {
	Name       *string `json:"name"`
	Version    *string `json:"version"`
	Type       *string `json:"type"`
	InstallCmd *string `json:"install_cmd"`
}

func (h *packagesHandler) handleListPackages(w http.ResponseWriter, r *http.Request) {
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
SELECT id, name, COALESCE(version, ''), type, COALESCE(install_cmd, ''), created_at, updated_at
FROM packages
ORDER BY name ASC, type ASC, version ASC
`)
	if err != nil {
		h.deps.Logger.Error("list packages", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to list packages")
		return
	}
	defer rows.Close()
	var out []packageWire
	for rows.Next() {
		var p packageWire
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &p.Type, &p.InstallCmd, &p.CreatedAt, &p.UpdatedAt); err != nil {
			h.deps.Logger.Error("scan package", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTicketsError(w, http.StatusInternalServerError, "failed to list packages")
			return
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list packages")
		return
	}
	if out == nil {
		out = []packageWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *packagesHandler) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPackageJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createPackageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.Version = strings.TrimSpace(req.Version)
	req.InstallCmd = strings.TrimSpace(req.InstallCmd)
	if req.Name == "" {
		writeTicketsError(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, ok := allowedPackageTypes[req.Type]; !ok {
		writeTicketsError(w, http.StatusBadRequest, "invalid type")
		return
	}
	if req.Type == "custom" && req.InstallCmd == "" {
		writeTicketsError(w, http.StatusBadRequest, "install_cmd is required for custom packages")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO packages (name, version, type, install_cmd)
VALUES ($1, $2, $3, $4)
RETURNING id
`, req.Name, req.Version, req.Type, req.InstallCmd).Scan(&id)
	if err != nil {
		h.deps.Logger.Error("insert package", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create package")
		return
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *packagesHandler) handleGetPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	id, err := parsePackageUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid package id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var p models.Package
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), type, COALESCE(install_cmd, ''), created_at, updated_at
FROM packages WHERE id = $1
`, id).Scan(&p.ID, &p.Name, &p.Version, &p.Type, &p.InstallCmd, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsError(w, http.StatusNotFound, "package not found")
		return
	}
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to load package")
		return
	}
	writeTicketsJSON(w, http.StatusOK, p)
}

func (h *packagesHandler) handlePatchPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parsePackageUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid package id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPackageJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchPackageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == nil && req.Version == nil && req.Type == nil && req.InstallCmd == nil {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if *req.Name == "" {
			writeTicketsError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
	}
	if req.Type != nil {
		t := strings.ToLower(strings.TrimSpace(*req.Type))
		if _, ok := allowedPackageTypes[t]; !ok {
			writeTicketsError(w, http.StatusBadRequest, "invalid type")
			return
		}
		req.Type = &t
	}
	if req.Version != nil {
		*req.Version = strings.TrimSpace(*req.Version)
	}
	if req.InstallCmd != nil {
		*req.InstallCmd = strings.TrimSpace(*req.InstallCmd)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var setParts []string
	args := make([]any, 0, 8)
	argPos := 1
	if req.Name != nil {
		setParts = append(setParts, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *req.Name)
		argPos++
	}
	if req.Version != nil {
		setParts = append(setParts, fmt.Sprintf("version = $%d", argPos))
		args = append(args, *req.Version)
		argPos++
	}
	if req.Type != nil {
		setParts = append(setParts, fmt.Sprintf("type = $%d", argPos))
		args = append(args, *req.Type)
		argPos++
	}
	if req.InstallCmd != nil {
		setParts = append(setParts, fmt.Sprintf("install_cmd = $%d", argPos))
		args = append(args, *req.InstallCmd)
		argPos++
	}
	setParts = append(setParts, "updated_at = now()")
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE packages SET %s WHERE id = $%d`, strings.Join(setParts, ", "), argPos)
	tag, err := h.deps.Pool.Exec(ctx, q, args...)
	if err != nil {
		h.deps.Logger.Error("patch package", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to update package")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "package not found")
		return
	}
	var p models.Package
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), type, COALESCE(install_cmd, ''), created_at, updated_at
FROM packages WHERE id = $1
`, id).Scan(&p.ID, &p.Name, &p.Version, &p.Type, &p.InstallCmd, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	writeTicketsJSON(w, http.StatusOK, p)
}

func (h *packagesHandler) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeAdmin(w, r) {
		return
	}
	id, err := parsePackageUUIDParam(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid package id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM packages WHERE id = $1`, id)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete package")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "package not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- deployments ---

type deploymentWire struct {
	ID             uuid.UUID `json:"id"`
	AssetID        uuid.UUID `json:"asset_id"`
	AssetHumanID   string    `json:"asset_human_id"`
	PackageID      uuid.UUID `json:"package_id"`
	PackageName    string    `json:"package_name"`
	PackageType    string    `json:"package_type"`
	PackageVersion string    `json:"package_version"`
	Status         string    `json:"status"`
	ErrorMessage   *string   `json:"error_message,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type createDeploymentRequest struct {
	AssetHumanID  string `json:"asset_human_id"`
	PackageID     string `json:"package_id"`
	TriggerDeploy bool   `json:"trigger_deploy"`
	Operation     string `json:"operation"` // install | uninstall (default install)
}

func (h *packagesHandler) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	assetHID := strings.TrimSpace(r.URL.Query().Get("asset_human_id"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var rows pgx.Rows
	var err error
	if assetHID != "" {
		rows, err = h.deps.Pool.Query(ctx, `
SELECT d.id, d.asset_id, a.human_id, d.package_id, p.name, p.type, COALESCE(p.version, ''),
       d.status, d.error_message, d.created_at, d.updated_at
FROM deployments d
JOIN assets a ON a.id = d.asset_id
JOIN packages p ON p.id = d.package_id
WHERE a.human_id = $1
ORDER BY d.created_at DESC
LIMIT 500
`, assetHID)
	} else {
		rows, err = h.deps.Pool.Query(ctx, `
SELECT d.id, d.asset_id, a.human_id, d.package_id, p.name, p.type, COALESCE(p.version, ''),
       d.status, d.error_message, d.created_at, d.updated_at
FROM deployments d
JOIN assets a ON a.id = d.asset_id
JOIN packages p ON p.id = d.package_id
ORDER BY d.created_at DESC
LIMIT 500
`)
	}
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}
	defer rows.Close()
	var out []deploymentWire
	for rows.Next() {
		var d deploymentWire
		if err := rows.Scan(&d.ID, &d.AssetID, &d.AssetHumanID, &d.PackageID, &d.PackageName, &d.PackageType, &d.PackageVersion,
			&d.Status, &d.ErrorMessage, &d.CreatedAt, &d.UpdatedAt); err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to list deployments")
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list deployments")
		return
	}
	if out == nil {
		out = []deploymentWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *packagesHandler) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPackageJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createDeploymentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.AssetHumanID = strings.TrimSpace(req.AssetHumanID)
	req.PackageID = strings.TrimSpace(req.PackageID)
	if req.AssetHumanID == "" || req.PackageID == "" {
		writeTicketsError(w, http.StatusBadRequest, "asset_human_id and package_id are required")
		return
	}
	op := strings.ToLower(strings.TrimSpace(req.Operation))
	if op == "" {
		op = "install"
	}
	if op != "install" && op != "uninstall" {
		writeTicketsError(w, http.StatusBadRequest, "operation must be install or uninstall")
		return
	}
	pkgID, err := uuid.Parse(req.PackageID)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid package_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var assetID uuid.UUID
	if err := h.deps.Pool.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, req.AssetHumanID).Scan(&assetID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusBadRequest, "asset not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
		return
	}

	var pkg models.Package
	if err := h.deps.Pool.QueryRow(ctx, `
SELECT id, name, COALESCE(version, ''), type, COALESCE(install_cmd, '')
FROM packages WHERE id = $1
`, pkgID).Scan(&pkg.ID, &pkg.Name, &pkg.Version, &pkg.Type, &pkg.InstallCmd); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusBadRequest, "package not found")
			return
		}
		writeTicketsError(w, http.StatusInternalServerError, "failed to load package")
		return
	}

	var depID uuid.UUID
	if err := h.deps.Pool.QueryRow(ctx, `
INSERT INTO deployments (asset_id, package_id, status)
VALUES ($1, $2, 'pending')
RETURNING id
`, assetID, pkg.ID).Scan(&depID); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to create deployment")
		return
	}

	dispatched := false
	var dispatchErr string
	if req.TriggerDeploy {
		if h.deps.C2Hub == nil {
			dispatchErr = "c2 hub not configured"
			_, _ = h.deps.Pool.Exec(ctx, `
UPDATE deployments SET status = 'failed', error_message = $2, updated_at = now() WHERE id = $1
`, depID, dispatchErr)
		} else {
			payload := map[string]any{
				"action":          "deploy_package",
				"deployment_id":   depID.String(),
				"operation":       op,
				"package_type":    pkg.Type,
				"name":            pkg.Name,
				"version":         pkg.Version,
				"install_cmd":     pkg.InstallCmd,
			}
			if err := h.deps.C2Hub.DispatchJSONByHumanID(ctx, h.deps.Pool, req.AssetHumanID, payload); err != nil {
				dispatchErr = err.Error()
				_, _ = h.deps.Pool.Exec(ctx, `
UPDATE deployments SET status = 'failed', error_message = $2, updated_at = now() WHERE id = $1
`, depID, dispatchErr)
			} else {
				_, _ = h.deps.Pool.Exec(ctx, `UPDATE deployments SET status = 'dispatched', updated_at = now() WHERE id = $1`, depID)
				dispatched = true
			}
		}
	}

	writeTicketsJSON(w, http.StatusCreated, map[string]any{
		"id":                 depID.String(),
		"triggered":          req.TriggerDeploy,
		"dispatch_succeeded": dispatched,
		"dispatch_error":     nullIfEmpty(dispatchErr),
	})
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
