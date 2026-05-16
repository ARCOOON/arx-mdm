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

const maxPrincipalGroupJSONBody = 64 << 10

// PrincipalGroupsDeps manages logical device cohort naming for profile assignment scopes.
type PrincipalGroupsDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type principalGroupsHandler struct {
	deps PrincipalGroupsDeps
}

// RegisterPrincipalGroupRoutes exposes CRUD and membership tooling for cohorts.
func RegisterPrincipalGroupRoutes(mux *http.ServeMux, d PrincipalGroupsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: principal groups routes require Pool, Logger, Auth.JWT")
	}
	h := &principalGroupsHandler{deps: d}
	mux.HandleFunc("GET /v1/principal-groups", h.handleList)
	mux.HandleFunc("POST /v1/principal-groups", h.handleCreate)
	mux.HandleFunc("GET /v1/principal-groups/{id}", h.handleGet)
	mux.HandleFunc("PATCH /v1/principal-groups/{id}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/principal-groups/{id}", h.handleDelete)
	mux.HandleFunc("POST /v1/principal-groups/{id}/devices", h.handleAddDevices)
	mux.HandleFunc("DELETE /v1/principal-groups/{id}/devices/{deviceId}", h.handleRemoveDevice)
}

func (h *principalGroupsHandler) parseGroupID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
}
	return uuid.Parse(raw)
}

type createPrincipalGroupReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type patchPrincipalGroupReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type bulkDeviceMembersReq struct {
	DeviceIDs []string `json:"device_ids"`
}

func (h *principalGroupsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, name, description, created_at
FROM principal_groups
ORDER BY lower(name) ASC
LIMIT 500
`)
	if err != nil {
		h.deps.Logger.Error("principal groups list", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load groups")
		return
}
	defer rows.Close()

	var list []models.PrincipalGroup
	for rows.Next() {
		var g models.PrincipalGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			h.deps.Logger.Error("principal groups scan", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to load groups")
			return
		}
		list = append(list, g)
	}
	if list == nil {
		list = []models.PrincipalGroup{}
	}
	writeTicketsJSON(w, http.StatusOK, list)
}

func (h *principalGroupsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPrincipalGroupJSONBody))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}
	var req createPrincipalGroupReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 200 {
		writeTicketsError(w, http.StatusBadRequest, "name is required")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var gid uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO principal_groups (name, description)
VALUES ($1, $2)
RETURNING id
`, req.Name, strings.TrimSpace(req.Description)).Scan(&gid)
	if err != nil {
		h.deps.Logger.Error("principal group insert", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create group")
		return
	}

	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": gid.String()})
}

func (h *principalGroupsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
}

	gid, err := h.parseGroupID(r)
	if err != nil || gid == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing group id")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var g models.PrincipalGroup
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, description, created_at
FROM principal_groups
WHERE id = $1::uuid
`, gid).Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt)
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, "group not found")
		return
	}

	deviceRows, err := h.deps.Pool.Query(ctx, `
SELECT device_id
FROM principal_group_devices
WHERE group_id = $1::uuid
ORDER BY created_at DESC
LIMIT 5000
`, gid)
	if err != nil {
		h.deps.Logger.Error("group devices query", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to enumerate members")
		return
}
	defer deviceRows.Close()

	var deviceIDs []string
	for deviceRows.Next() {
		var did uuid.UUID
		if err := deviceRows.Scan(&did); err != nil {
			h.deps.Logger.Error("group device scan", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to enumerate members")
			return
		}
		deviceIDs = append(deviceIDs, did.String())
}
	if deviceIDs == nil {
		deviceIDs = []string{}
}

	writeTicketsJSON(w, http.StatusOK, map[string]any{
		"group":      g,
		"device_ids": deviceIDs,
	})
}

func (h *principalGroupsHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	gid, err := h.parseGroupID(r)
	if err != nil || gid == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing group id")
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPrincipalGroupJSONBody))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}

	var patch patchPrincipalGroupReq
	if err := json.Unmarshal(body, &patch); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" || utf8.RuneCountInString(name) > 200 {
			writeTicketsError(w, http.StatusBadRequest, "invalid name")
			return
}
		res, execErr := h.deps.Pool.Exec(ctx, `
UPDATE principal_groups SET name=$2 WHERE id=$1::uuid
`, gid, name)
		if execErr != nil {
			h.deps.Logger.Error("principal group rename", "err", execErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to update group")
			return
}
		if res.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "group not found")
			return
}
	}

	if patch.Description != nil {
		res, execErr := h.deps.Pool.Exec(ctx, `
UPDATE principal_groups SET description=$2 WHERE id=$1::uuid
`, gid, strings.TrimSpace(*patch.Description))
		if execErr != nil {
			h.deps.Logger.Error("principal group description", "err", execErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to update group")
			return
}
		if res.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "group not found")
			return
}
	}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *principalGroupsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	gid, err := h.parseGroupID(r)
	if err != nil || gid == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing group id")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, execErr := h.deps.Pool.Exec(ctx, `DELETE FROM principal_groups WHERE id=$1::uuid`, gid)
	if execErr != nil {
		h.deps.Logger.Error("principal group delete", "err", execErr)
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete group")
		return
}
	if res.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "group not found")
		return
}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *principalGroupsHandler) handleAddDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	gid, err := h.parseGroupID(r)
	if err != nil || gid == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing group id")
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPrincipalGroupJSONBody))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}

	var bulk bulkDeviceMembersReq
	if err := json.Unmarshal(body, &bulk); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}
	if len(bulk.DeviceIDs) == 0 {
		writeTicketsError(w, http.StatusBadRequest, "device_ids is required")
		return
}

	var deviceUUIDs []uuid.UUID
	for _, raw := range bulk.DeviceIDs {
		id, parseErr := uuid.Parse(strings.TrimSpace(raw))
		if parseErr != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid device id")
			return
		}
		deviceUUIDs = append(deviceUUIDs, id)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var exists int
	if err := h.deps.Pool.QueryRow(ctx, `SELECT 1 FROM principal_groups WHERE id=$1::uuid`, gid).Scan(&exists); err != nil {
		writeTicketsError(w, http.StatusNotFound, "group not found")
		return
}

	inserted := 0
	for _, dev := range deviceUUIDs {
		tag, execErr := h.deps.Pool.Exec(ctx, `
INSERT INTO principal_group_devices (group_id, device_id)
VALUES ($1::uuid, $2::uuid)
ON CONFLICT DO NOTHING
`, gid, dev)
		if execErr != nil {
			h.deps.Logger.Error("principal group membership insert", "err", execErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to add devices")
			return
	}
		inserted += int(tag.RowsAffected())
	}

	writeTicketsJSON(w, http.StatusOK, map[string]any{
		"status":               "ok",
		"memberships_inserted": inserted,
	})
}

func (h *principalGroupsHandler) handleRemoveDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	gid, err := h.parseGroupID(r)
	if err != nil || gid == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "missing group id")
		return
}

	rawDev := strings.TrimSpace(r.PathValue("deviceId"))
	did, err := uuid.Parse(rawDev)
	if err != nil || did == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, execErr := h.deps.Pool.Exec(ctx, `
DELETE FROM principal_group_devices
WHERE group_id = $1::uuid AND device_id = $2::uuid
`, gid, did)
	if execErr != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to detach device")
		return
}
	if res.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "membership not found")
		return
}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
