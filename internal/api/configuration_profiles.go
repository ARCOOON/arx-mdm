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
	"unicode/utf8"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxConfigurationProfileJSON = 768 << 10

// ConfigurationProfilesDeps controls declarative payloads applied to fleets.
type ConfigurationProfilesDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type configurationProfilesHandler struct {
	deps ConfigurationProfilesDeps
}

// RegisterConfigurationProfilesRoutes registers CRUD endpoints for configuration profiles plus assignment flows.
func RegisterConfigurationProfilesRoutes(mux *http.ServeMux, d ConfigurationProfilesDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: configuration profiles routes require Pool, Logger, Auth.JWT")
}
	h := &configurationProfilesHandler{deps: d}
	mux.HandleFunc("GET /v1/configuration-profiles", h.handleListProfiles)
	mux.HandleFunc("POST /v1/configuration-profiles", h.handleCreateProfile)
	mux.HandleFunc("GET /v1/configuration-profiles/{id}", h.handleGetProfile)
	mux.HandleFunc("PATCH /v1/configuration-profiles/{id}", h.handlePatchProfile)
	mux.HandleFunc("DELETE /v1/configuration-profiles/{id}", h.handleDeleteProfile)
	mux.HandleFunc("GET /v1/configuration-profiles/{id}/assignments", h.handleListAssignments)
	mux.HandleFunc("POST /v1/configuration-profiles/{id}/assignments", h.handleAssignProfile)
	mux.HandleFunc("DELETE /v1/profile-assignments/{assignmentId}", h.handleRemoveAssignment)
}

func parseProfileUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing profile id")
	}
	return uuid.Parse(raw)
}

type createProfileRequest struct {
	Name     string          `json:"name"`
	Platform string          `json:"platform"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

type patchProfileRequest struct {
	Name     *string         `json:"name"`
	Type     *string         `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Platform *string         `json:"platform"`
}

type assignProfileRequest struct {
	TargetKind         string `json:"target_kind"`
	DeviceID           string `json:"device_id"`
	PrincipalGroupID   string `json:"principal_group_id"`
}

func allowedPlatform(in string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "windows", "linux", "android":
		return strings.ToLower(strings.TrimSpace(in)), true
	default:
		return "", false
	}
}

func (h *configurationProfilesHandler) handleListProfiles(w http.ResponseWriter, r *http.Request) {
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
SELECT id, name, platform, type, payload, created_at
FROM configuration_profiles
ORDER BY created_at DESC
LIMIT 800
`)
	if err != nil {
		h.deps.Logger.Error("configuration profiles list", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list profiles")
		return
}
	defer rows.Close()

	var list []models.ConfigurationProfile
	for rows.Next() {
		var p models.ConfigurationProfile
		var payload []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Platform, &p.Type, &payload, &p.CreatedAt); err != nil {
			h.deps.Logger.Error("configuration profile scan", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to list profiles")
			return
}
		if len(payload) == 0 {
			p.Payload = json.RawMessage(`{}`)
		} else {
			p.Payload = json.RawMessage(payload)
		}
		list = append(list, p)
}
	if list == nil {
		list = []models.ConfigurationProfile{}
	}
	writeTicketsJSON(w, http.StatusOK, list)
}

func (h *configurationProfilesHandler) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigurationProfileJSON))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}
	var req createProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}
	req.Name = strings.TrimSpace(req.Name)
	ptype := strings.TrimSpace(req.Type)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 200 || ptype == "" {
		writeTicketsError(w, http.StatusBadRequest, "name and type are required")
		return
}

	platform, ok := allowedPlatform(req.Platform)
	if !ok {
		writeTicketsError(w, http.StatusBadRequest, "platform must be windows, linux, or android")
		return
}

	payload := req.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO configuration_profiles (name, platform, type, payload)
VALUES ($1, $2, $3, $4::jsonb)
RETURNING id
`, req.Name, platform, ptype, string(payload)).Scan(&id)
	if err != nil {
		h.deps.Logger.Error("configuration profile insert", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create profile")
		return
}

	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *configurationProfilesHandler) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
}

	pid, err := parseProfileUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var p models.ConfigurationProfile
	var payload []byte
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, name, platform, type, payload, created_at
FROM configuration_profiles
WHERE id = $1::uuid
`, pid).Scan(&p.ID, &p.Name, &p.Platform, &p.Type, &payload, &p.CreatedAt)
	if err != nil {
		writeTicketsError(w, http.StatusNotFound, "profile not found")
		return
}
	if len(payload) == 0 {
		p.Payload = json.RawMessage(`{}`)
	} else {
		p.Payload = json.RawMessage(payload)
	}

	writeTicketsJSON(w, http.StatusOK, p)
}

func (h *configurationProfilesHandler) handlePatchProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	pid, err := parseProfileUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigurationProfileJSON))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}

	var patch patchProfileRequest
	if err := json.Unmarshal(body, &patch); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	name := ""
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
		if name == "" || utf8.RuneCountInString(name) > 200 {
			writeTicketsError(w, http.StatusBadRequest, "invalid name")
			return
}
		tag, execErr := h.deps.Pool.Exec(ctx, `UPDATE configuration_profiles SET name=$2 WHERE id=$1::uuid`, pid, name)
		if execErr != nil || tag.RowsAffected() == 0 {
			if execErr != nil {
				h.deps.Logger.Error("profile patch name", "err", execErr)
			}
			writeTicketsError(w, http.StatusNotFound, "profile not found")
			return
		}
	}

	if patch.Type != nil {
		t := strings.TrimSpace(*patch.Type)
		if t == "" {
			writeTicketsError(w, http.StatusBadRequest, "invalid type")
			return
}
		tag, execErr := h.deps.Pool.Exec(ctx, `UPDATE configuration_profiles SET type=$2 WHERE id=$1::uuid`, pid, t)
		if execErr != nil || tag.RowsAffected() == 0 {
			if execErr != nil {
				h.deps.Logger.Error("profile patch type", "err", execErr)
			}
			writeTicketsError(w, http.StatusNotFound, "profile not found")
			return
		}
	}

	if patch.Platform != nil {
		platform, ok := allowedPlatform(*patch.Platform)
		if !ok {
			writeTicketsError(w, http.StatusBadRequest, "invalid platform")
			return
}
		tag, execErr := h.deps.Pool.Exec(ctx, `UPDATE configuration_profiles SET platform=$2 WHERE id=$1::uuid`, pid, platform)
		if execErr != nil || tag.RowsAffected() == 0 {
			if execErr != nil {
				h.deps.Logger.Error("profile patch platform", "err", execErr)
			}
			writeTicketsError(w, http.StatusNotFound, "profile not found")
			return
		}
	}

	if patch.Payload != nil && len(patch.Payload) != 0 {
		tag, execErr := h.deps.Pool.Exec(ctx, `UPDATE configuration_profiles SET payload=$2::jsonb WHERE id=$1::uuid`, pid, string(patch.Payload))
		if execErr != nil || tag.RowsAffected() == 0 {
			if execErr != nil {
				h.deps.Logger.Error("profile patch payload", "err", execErr)
			}
			writeTicketsError(w, http.StatusNotFound, "profile not found")
			return
		}
	}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *configurationProfilesHandler) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	pid, err := parseProfileUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tag, execErr := h.deps.Pool.Exec(ctx, `DELETE FROM configuration_profiles WHERE id=$1::uuid`, pid)
	if execErr != nil {
		h.deps.Logger.Error("configuration profile delete", "err", execErr)
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete profile")
		return
}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "profile not found")
		return
}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *configurationProfilesHandler) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
}

	pid, err := parseProfileUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, profile_id, target_kind, device_id, principal_group_id, created_at, assignment_state
FROM profile_assignments
WHERE profile_id = $1::uuid
ORDER BY created_at DESC
LIMIT 8000
`, pid)
	if err != nil {
		h.deps.Logger.Error("list profile assignments", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list assignments")
		return
}
	defer rows.Close()

	var list []models.ProfileAssignment
	for rows.Next() {
		var pa models.ProfileAssignment
		var dev pgtype.UUID
		var grp pgtype.UUID
		if err := rows.Scan(&pa.ID, &pa.ProfileID, &pa.TargetKind, &dev, &grp, &pa.CreatedAt, &pa.AssignmentState); err != nil {
			h.deps.Logger.Error("scan assignment row", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to list assignments")
			return
	}
		if dev.Valid {
			u := uuid.UUID(dev.Bytes)
			copyID := u
			pa.DeviceID = &copyID
		}
		if grp.Valid {
			u := uuid.UUID(grp.Bytes)
			copyID := u
			pa.PrincipalGroupID = &copyID
		}
		list = append(list, pa)
}
	if list == nil {
		list = []models.ProfileAssignment{}
}

	writeTicketsJSON(w, http.StatusOK, list)
}

func (h *configurationProfilesHandler) handleAssignProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	pid, err := parseProfileUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigurationProfileJSON))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "unable to read body")
		return
}

	var req assignProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
}

	target := strings.TrimSpace(strings.ToLower(req.TargetKind))
	if target != "device" && target != "principal_group" {
		writeTicketsError(w, http.StatusBadRequest, "target_kind must be device or principal_group")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	var exists int
	if err := h.deps.Pool.QueryRow(ctx, `SELECT 1 FROM configuration_profiles WHERE id=$1::uuid`, pid).Scan(&exists); err != nil {
		writeTicketsError(w, http.StatusNotFound, "profile not found")
		return
}

	var assignmentID uuid.UUID
	switch target {
	case "device":
		did, parseErr := uuid.Parse(strings.TrimSpace(req.DeviceID))
		if parseErr != nil || did == uuid.Nil {
			writeTicketsError(w, http.StatusBadRequest, "device_id is required")
			return
		}
		scanErr := h.deps.Pool.QueryRow(ctx, `
INSERT INTO profile_assignments (profile_id, target_kind, device_id)
VALUES ($1::uuid, 'device', $2::uuid)
ON CONFLICT DO NOTHING
RETURNING id
`, pid, did).Scan(&assignmentID)
		if scanErr != nil && !errors.Is(scanErr, pgx.ErrNoRows) {
			h.deps.Logger.Error("assign profile device", "err", scanErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to link device assignment")
			return
		}
		if errors.Is(scanErr, pgx.ErrNoRows) {
			dupErr := h.deps.Pool.QueryRow(ctx, `
SELECT id FROM profile_assignments
WHERE profile_id=$1::uuid AND target_kind='device' AND device_id=$2::uuid
LIMIT 1
`, pid, did).Scan(&assignmentID)
			if dupErr != nil {
				writeTicketsError(w, http.StatusConflict, "duplicate assignment or invalid asset")
				return
			}
		}

	default:
		gid, parseErr := uuid.Parse(strings.TrimSpace(req.PrincipalGroupID))
		if parseErr != nil || gid == uuid.Nil {
			writeTicketsError(w, http.StatusBadRequest, "principal_group_id is required")
			return
		}
		scanErr := h.deps.Pool.QueryRow(ctx, `
INSERT INTO profile_assignments (profile_id, target_kind, principal_group_id)
VALUES ($1::uuid, 'principal_group', $2::uuid)
ON CONFLICT DO NOTHING
RETURNING id
`, pid, gid).Scan(&assignmentID)
		if scanErr != nil && !errors.Is(scanErr, pgx.ErrNoRows) {
			h.deps.Logger.Error("assign profile group", "err", scanErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to link cohort assignment")
			return
		}
		if errors.Is(scanErr, pgx.ErrNoRows) {
			dupErr := h.deps.Pool.QueryRow(ctx, `
SELECT id FROM profile_assignments
WHERE profile_id=$1::uuid AND target_kind='principal_group' AND principal_group_id=$2::uuid
LIMIT 1
`, pid, gid).Scan(&assignmentID)
			if dupErr != nil {
				writeTicketsError(w, http.StatusConflict, "duplicate assignment or invalid cohort")
				return
			}
		}
	}

	writeTicketsJSON(w, http.StatusCreated, map[string]string{"assignment_id": assignmentID.String()})
}

func (h *configurationProfilesHandler) handleRemoveAssignment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
}

	raw := strings.TrimSpace(r.PathValue("assignmentId"))
	id, parseErr := uuid.Parse(raw)
	if parseErr != nil || id == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "assignment id invalid")
		return
}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tag, execErr := h.deps.Pool.Exec(ctx, `DELETE FROM profile_assignments WHERE id=$1::uuid`, id)
	if execErr != nil || tag.RowsAffected() == 0 {
		if execErr != nil {
			h.deps.Logger.Error("remove assignment", "err", execErr)
			writeTicketsError(w, http.StatusInternalServerError, "failed to remove assignment")
			return
}
		writeTicketsError(w, http.StatusNotFound, "assignment missing")
		return
}

	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
