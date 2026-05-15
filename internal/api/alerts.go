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

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/notifications"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAlertJSONBody = 64 << 10

// AlertsDeps wires alert settings REST.
type AlertsDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Auth    DashboardAuth
	Alerter *notifications.Alerter
}

type alertSettingDTO struct {
	ID         uuid.UUID      `json:"id"`
	Type       string         `json:"type"`
	Config     map[string]any `json:"config"`
	IsActive   bool           `json:"is_active"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type alertsJSONError struct {
	Error string `json:"error"`
}

func writeAlertsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAlertsError(w http.ResponseWriter, status int, msg string) {
	writeAlertsJSON(w, status, alertsJSONError{Error: msg})
}

func maskAlertConfig(typ string, cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	if typ == notifications.TypeSMTP {
		if _, ok := out["password"]; ok {
			out["password"] = ""
		}
	}
	return out
}

func mergeSMTPPasswordPreserve(existing map[string]any, patch map[string]any) map[string]any {
	if existing == nil {
		existing = map[string]any{}
	}
	if patch == nil {
		patch = map[string]any{}
	}
	oldPass := ""
	if v, ok := existing["password"]; ok {
		if s, _ := v.(string); s != "" {
			oldPass = s
		}
	}
	out := make(map[string]any, len(existing)+len(patch))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range patch {
		out[k] = v
	}
	if s, ok := out["password"].(string); ok && strings.TrimSpace(s) == "" && oldPass != "" {
		out["password"] = oldPass
	}
	return out
}

// RegisterAlertRoutes registers dashboard alert configuration (admin-only).
func RegisterAlertRoutes(mux *http.ServeMux, d AlertsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil || d.Alerter == nil {
		panic("api: alerts routes require Pool, Logger, Auth.JWT, and Alerter")
	}
	h := &alertsHandler{deps: d}
	mux.HandleFunc("GET /v1/alerts/settings", h.handleList)
	mux.HandleFunc("POST /v1/alerts/settings", h.handleCreate)
	mux.HandleFunc("PATCH /v1/alerts/settings/{id}", h.handlePatch)
	mux.HandleFunc("POST /v1/alerts/settings/{id}/test", h.handleTest)
}

type alertsHandler struct {
	deps AlertsDeps
}

func (h *alertsHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin)
	return ok
}

func (h *alertsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAlertsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, type, config_json, is_active, created_at, updated_at
FROM alert_settings
ORDER BY type ASC, created_at ASC
`)
	if err != nil {
		h.deps.Logger.Error("list alert_settings", "err", err)
		writeAlertsError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	defer rows.Close()
	var out []alertSettingDTO
	for rows.Next() {
		var row alertSettingDTO
		var cfgBytes []byte
		if err := rows.Scan(&row.ID, &row.Type, &cfgBytes, &row.IsActive, &row.CreatedAt, &row.UpdatedAt); err != nil {
			h.deps.Logger.Error("scan alert_settings", "err", err)
			writeAlertsError(w, http.StatusInternalServerError, "failed to list settings")
			return
		}
		var cfg map[string]any
		if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
			cfg = map[string]any{}
		}
		row.Config = maskAlertConfig(row.Type, cfg)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		h.deps.Logger.Error("alert_settings rows", "err", err)
		writeAlertsError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	if out == nil {
		out = []alertSettingDTO{}
	}
	writeAlertsJSON(w, http.StatusOK, out)
}

type createAlertSettingRequest struct {
	Type     string         `json:"type"`
	Config   map[string]any `json:"config"`
	IsActive *bool          `json:"is_active"`
}

func (h *alertsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeAlertsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createAlertSettingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAlertsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t := strings.ToLower(strings.TrimSpace(req.Type))
	if t != notifications.TypeSMTP && t != notifications.TypeWebhook {
		writeAlertsError(w, http.StatusBadRequest, "type must be smtp or webhook")
		return
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	cfgBytes, err := json.Marshal(req.Config)
	if err != nil {
		writeAlertsError(w, http.StatusBadRequest, "invalid config")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var newID uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO alert_settings (type, config_json, is_active)
VALUES ($1, $2::jsonb, $3)
RETURNING id
`, t, string(cfgBytes), active).Scan(&newID)
	if err != nil {
		h.deps.Logger.Error("insert alert_settings", "err", err)
		writeAlertsError(w, http.StatusInternalServerError, "failed to save setting")
		return
	}
	writeAlertsJSON(w, http.StatusCreated, map[string]string{"id": newID.String()})
}

type patchAlertSettingRequest struct {
	Config   map[string]any `json:"config"`
	IsActive *bool          `json:"is_active"`
}

func (h *alertsHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeAlertsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	idRaw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(idRaw)
	if err != nil {
		writeAlertsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeAlertsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchAlertSettingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAlertsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Config == nil && req.IsActive == nil {
		writeAlertsError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var typ string
	var curJSON []byte
	var curActive bool
	err = h.deps.Pool.QueryRow(ctx, `
SELECT type, config_json::text, is_active FROM alert_settings WHERE id = $1
`, id).Scan(&typ, &curJSON, &curActive)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAlertsError(w, http.StatusNotFound, "setting not found")
		return
	}
	if err != nil {
		h.deps.Logger.Error("load alert_settings", "err", err)
		writeAlertsError(w, http.StatusInternalServerError, "failed to update setting")
		return
	}

	var nextCfg map[string]any
	if req.Config != nil {
		var cur map[string]any
		_ = json.Unmarshal(curJSON, &cur)
		if typ == notifications.TypeSMTP {
			nextCfg = mergeSMTPPasswordPreserve(cur, req.Config)
		} else {
			nextCfg = req.Config
		}
	}
	nextActive := curActive
	if req.IsActive != nil {
		nextActive = *req.IsActive
	}

	var cfgArg any
	if nextCfg != nil {
		b, err := json.Marshal(nextCfg)
		if err != nil {
			writeAlertsError(w, http.StatusBadRequest, "invalid config")
			return
		}
		cfgArg = string(b)
	}

	if nextCfg != nil && req.IsActive != nil {
		_, err = h.deps.Pool.Exec(ctx, `
UPDATE alert_settings
SET config_json = $2::jsonb, is_active = $3, updated_at = now()
WHERE id = $1
`, id, cfgArg, nextActive)
	} else if nextCfg != nil {
		_, err = h.deps.Pool.Exec(ctx, `
UPDATE alert_settings
SET config_json = $2::jsonb, updated_at = now()
WHERE id = $1
`, id, cfgArg)
	} else {
		_, err = h.deps.Pool.Exec(ctx, `
UPDATE alert_settings
SET is_active = $2, updated_at = now()
WHERE id = $1
`, id, nextActive)
	}
	if err != nil {
		h.deps.Logger.Error("update alert_settings", "err", err)
		writeAlertsError(w, http.StatusInternalServerError, "failed to update setting")
		return
	}
	writeAlertsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *alertsHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	idRaw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(idRaw)
	if err != nil {
		writeAlertsError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := h.deps.Alerter.SendTest(ctx, id); err != nil {
		h.deps.Logger.Warn("alert test failed", "err", err, "setting_id", id.String())
		writeAlertsError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAlertsJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
