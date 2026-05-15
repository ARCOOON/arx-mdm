package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"arx-mdm/internal/auth"
	"arx-mdm/internal/scheduler"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAutomationJSONBody = 64 << 10

// AutomationsDeps wires /v1/automations CRUD for scheduled C2 jobs.
type AutomationsDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type automationWire struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	CronSchedule  string          `json:"cron_schedule"`
	ActionType    string          `json:"action_type"`
	TargetOS      *string         `json:"target_os,omitempty"`
	TargetAssetID *uuid.UUID      `json:"target_asset_id,omitempty"`
	PayloadJSON   json.RawMessage `json:"payload_json"`
	IsActive      bool            `json:"is_active"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type createAutomationRequest struct {
	Name          string          `json:"name"`
	CronSchedule  string          `json:"cron_schedule"`
	ActionType    string          `json:"action_type"`
	TargetOS      *string         `json:"target_os"`
	TargetAssetID *string         `json:"target_asset_id"`
	PayloadJSON   json.RawMessage `json:"payload_json"`
	IsActive      *bool           `json:"is_active"`
}

type patchAutomationRequest struct {
	IsActive *bool `json:"is_active"`
}

// RegisterAutomationsRoutes registers dashboard automation APIs.
func RegisterAutomationsRoutes(mux *http.ServeMux, d AutomationsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: automations routes require Pool, Logger, and Auth.JWT")
	}
	h := &automationsHandler{deps: d}
	mux.HandleFunc("GET /v1/automations", h.handleList)
	mux.HandleFunc("POST /v1/automations", h.handleCreate)
	mux.HandleFunc("PATCH /v1/automations/{id}", h.handlePatch)
}

type automationsHandler struct {
	deps AutomationsDeps
}

func (h *automationsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, name, cron_schedule, action_type, target_os, target_asset_id, payload_json, is_active, created_at, updated_at
FROM automations
ORDER BY created_at DESC
LIMIT 500
`)
	if err != nil {
		h.deps.Logger.Error("list automations", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list automations")
		return
	}
	defer rows.Close()
	var out []automationWire
	for rows.Next() {
		var wv automationWire
		var payload []byte
		if err := rows.Scan(&wv.ID, &wv.Name, &wv.CronSchedule, &wv.ActionType, &wv.TargetOS, &wv.TargetAssetID, &payload, &wv.IsActive, &wv.CreatedAt, &wv.UpdatedAt); err != nil {
			h.deps.Logger.Error("scan automation", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to list automations")
			return
		}
		if len(payload) == 0 {
			wv.PayloadJSON = json.RawMessage(`{}`)
		} else {
			wv.PayloadJSON = json.RawMessage(payload)
		}
		out = append(out, wv)
	}
	if out == nil {
		out = []automationWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *automationsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAutomationJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createAutomationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.CronSchedule = strings.TrimSpace(req.CronSchedule)
	req.ActionType = strings.ToLower(strings.TrimSpace(req.ActionType))
	if req.Name == "" || req.CronSchedule == "" || req.ActionType == "" {
		writeTicketsError(w, http.StatusBadRequest, "name, cron_schedule, and action_type are required")
		return
	}
	if req.ActionType != "shutdown" && req.ActionType != "deploy_package" {
		writeTicketsError(w, http.StatusBadRequest, "action_type must be shutdown or deploy_package")
		return
	}
	if err := scheduler.ValidateCronSchedule(req.CronSchedule); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid cron_schedule: "+err.Error())
		return
	}
	if utf8.RuneCountInString(req.Name) > 200 {
		writeTicketsError(w, http.StatusBadRequest, "name too long")
		return
	}

	var targetOS *string
	var targetAssetID *uuid.UUID
	if req.TargetAssetID != nil && strings.TrimSpace(*req.TargetAssetID) != "" {
		aid, err := uuid.Parse(strings.TrimSpace(*req.TargetAssetID))
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid target_asset_id")
			return
		}
		targetAssetID = &aid
	} else if req.TargetOS != nil && strings.TrimSpace(*req.TargetOS) != "" {
		os := strings.ToLower(strings.TrimSpace(*req.TargetOS))
		if !allowedAutomationTargetOS[os] {
			writeTicketsError(w, http.StatusBadRequest, "invalid target_os")
			return
		}
		targetOS = &os
	} else {
		writeTicketsError(w, http.StatusBadRequest, "target_asset_id or target_os is required")
		return
	}

	payload := req.PayloadJSON
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	} else if !json.Valid(payload) {
		writeTicketsError(w, http.StatusBadRequest, "payload_json must be valid JSON")
		return
	}
	if req.ActionType == "deploy_package" {
		var probe struct {
			PackageID string `json:"package_id"`
		}
		if err := json.Unmarshal(payload, &probe); err != nil || strings.TrimSpace(probe.PackageID) == "" {
			writeTicketsError(w, http.StatusBadRequest, "deploy_package requires payload_json.package_id")
			return
		}
	}

	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO automations (name, cron_schedule, action_type, target_os, target_asset_id, payload_json, is_active)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
RETURNING id
`, req.Name, req.CronSchedule, req.ActionType, targetOS, targetAssetID, string(payload), active).Scan(&id)
	if err != nil {
		h.deps.Logger.Error("insert automation", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create automation")
		return
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

var allowedAutomationTargetOS = map[string]bool{
	"unknown": true, "windows": true, "linux": true, "darwin": true, "android": true, "ios": true,
}

func (h *automationsHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	rawID := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAutomationJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchAutomationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.IsActive == nil {
		writeTicketsError(w, http.StatusBadRequest, "is_active is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tag, err := h.deps.Pool.Exec(ctx, `
UPDATE automations SET is_active = $2, updated_at = now() WHERE id = $1
`, id, *req.IsActive)
	if err != nil {
		h.deps.Logger.Error("patch automation", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to update automation")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "automation not found")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]any{"ok": true})
}
