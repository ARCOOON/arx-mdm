package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxManagedAppConfigJSONBody = 64 << 10

// AppManagedConfigDeps nests App Catalog managed configuration authoring.
type AppManagedConfigDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type appManagedConfigHandler struct {
	deps AppManagedConfigDeps
}

// RegisterManagedAppConfigurationRoutes nests App Configuration JSON under catalog identifiers.
func RegisterManagedAppConfigurationRoutes(mux *http.ServeMux, d AppManagedConfigDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: managed app configuration routes require Pool, Logger, Auth.JWT")
	}
	h := &appManagedConfigHandler{deps: d}
	mux.HandleFunc("GET /v1/app-catalog/{catalogId}/managed-configurations", h.handleList)
	mux.HandleFunc("POST /v1/app-catalog/{catalogId}/managed-configurations", h.handleCreate)
	mux.HandleFunc("PATCH /v1/app-catalog/{catalogId}/managed-configurations/{configId}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/app-catalog/{catalogId}/managed-configurations/{configId}", h.handleDelete)
}

func parseCatalogUUIDParamCatalog(r *http.Request, key string) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue(key))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing %s", key)
	}
	return uuid.Parse(raw)
}

type createManagedConfigRequest struct {
	ManagedPackageName string          `json:"managed_package_name"`
	ManagedAppLabel    string          `json:"managed_app_label"`
	ConfigKV           json.RawMessage `json:"config_kv"`
}

type patchManagedConfigRequest struct {
	ManagedPackageName *string         `json:"managed_package_name"`
	ManagedAppLabel    *string         `json:"managed_app_label"`
	ConfigKV           json.RawMessage `json:"config_kv"`
}

func (h *appManagedConfigHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}

	catID, err := parseCatalogUUIDParamCatalog(r, "catalogId")
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, catalog_app_id, managed_package_name, managed_app_label, config_kv, created_at
FROM app_configurations
WHERE catalog_app_id=$1::uuid
ORDER BY lower(managed_package_name) ASC
LIMIT 250
`, catID)
	if err != nil {
		h.deps.Logger.Error("list managed configs", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list configs")
		return
	}
	defer rows.Close()

	var out []models.AppManagedConfiguration
	for rows.Next() {
		var m models.AppManagedConfiguration
		var kv []byte
		if err := rows.Scan(&m.ID, &m.CatalogAppID, &m.ManagedPackageName, &m.ManagedAppLabel, &kv, &m.CreatedAt); err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to enumerate configs")
			return
}
		m.ConfigKV = json.RawMessage(kv)
		out = append(out, m)
	}
	if out == nil {
		out = []models.AppManagedConfiguration{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *appManagedConfigHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}

	catID, err := parseCatalogUUIDParamCatalog(r, "catalogId")
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxManagedAppConfigJSONBody))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
	}

	var req createManagedConfigRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	pkg := strings.TrimSpace(req.ManagedPackageName)
	if pkg == "" || utf8.RuneCountInString(pkg) > 512 {
		writeTicketsError(w, http.StatusBadRequest, "managed_package_name is required")
		return
}

	kv := req.ConfigKV
	if len(kv) == 0 {
		kv = json.RawMessage(`{}`)
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var exists int
	if err := h.deps.Pool.QueryRow(ctx, `SELECT 1 FROM app_catalog WHERE id=$1::uuid`, catID).Scan(&exists); err != nil {
		writeTicketsError(w, http.StatusNotFound, "catalog entry missing")
		return
	}

	var newID uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO app_configurations (catalog_app_id, managed_package_name, managed_app_label, config_kv)
VALUES ($1::uuid, $2, $3, $4::jsonb)
RETURNING id
`, catID, pkg, strings.TrimSpace(req.ManagedAppLabel), string(kv)).Scan(&newID)
	if err != nil {
		h.deps.Logger.Error("managed app config insert", "err", err)
		writeTicketsError(w, http.StatusConflict, "package mapping already registered for this catalog app")
		return
	}

	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": newID.String()})
}

func (h *appManagedConfigHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}

	catID, err := parseCatalogUUIDParamCatalog(r, "catalogId")
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawCfg := strings.TrimSpace(r.PathValue("configId"))
	cfgID, err := uuid.Parse(rawCfg)
	if err != nil || cfgID == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing configuration id")
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxManagedAppConfigJSONBody))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
	}

	var patch patchManagedConfigRequest
	if err := json.Unmarshal(body, &patch); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	if patch.ManagedPackageName != nil {
		pkg := strings.TrimSpace(*patch.ManagedPackageName)
		if pkg == "" {
			writeTicketsError(w, http.StatusBadRequest, "managed_package_name invalid")
			return
}
		tag, execErr := h.deps.Pool.Exec(ctx, `
UPDATE app_configurations
SET managed_package_name=$3
WHERE id=$2::uuid AND catalog_app_id=$1::uuid
`, catID, cfgID, pkg)
		if execErr != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to rename configuration")
			return
}
		if tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "configuration missing")
			return
	}
	}

	if patch.ManagedAppLabel != nil {
		tag, execErr := h.deps.Pool.Exec(ctx, `
UPDATE app_configurations
SET managed_app_label=$3
WHERE id=$2::uuid AND catalog_app_id=$1::uuid
`, catID, cfgID, strings.TrimSpace(*patch.ManagedAppLabel))
		if execErr != nil || tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "configuration missing")
			return
	}
	}

	if patch.ConfigKV != nil && json.Valid(patch.ConfigKV) {
		tag, execErr := h.deps.Pool.Exec(ctx, `
UPDATE app_configurations
SET config_kv=$3::jsonb
WHERE id=$2::uuid AND catalog_app_id=$1::uuid
`, catID, cfgID, string(patch.ConfigKV))
		if execErr != nil || tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "configuration missing")
			return
	}
	}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *appManagedConfigHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}

	catID, err := parseCatalogUUIDParamCatalog(r, "catalogId")
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawCfg := strings.TrimSpace(r.PathValue("configId"))
	cfgID, err := uuid.Parse(rawCfg)
	if err != nil || cfgID == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing configuration id")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	tag, execErr := h.deps.Pool.Exec(ctx, `
DELETE FROM app_configurations
WHERE id=$2::uuid AND catalog_app_id=$1::uuid
`, catID, cfgID)
	if execErr != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete configuration")
		return
}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "configuration missing")
		return
}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
