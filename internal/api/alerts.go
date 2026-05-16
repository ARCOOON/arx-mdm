package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"
	"github.com/ARCOOON/arx-mdm/internal/notifications"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAlertJSONBody = 64 << 10

// AlertsDeps attaches persisted alerting primitives to REST handlers.
type AlertsDeps struct {
	Pool       *pgxpool.Pool
	Logger     *slog.Logger
	Auth       DashboardAuth
	Dispatcher *notifications.Dispatcher
}

type alertsJSONErr struct {
	Error string `json:"error"`
}

func writeAlertsPayload(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAlertsErr(w http.ResponseWriter, status int, msg string) {
	writeAlertsPayload(w, status, alertsJSONErr{Error: msg})
}

type notificationChannelDTO struct {
	ID                      uuid.UUID `json:"id"`
	Name                    string    `json:"name"`
	ChannelType             string    `json:"channel_type"`
	Config                  any       `json:"config"`
	IsActive                bool      `json:"is_active"`
	SigningSecretConfigured bool      `json:"signing_secret_configured"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type notificationChannelPayload struct {
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	ChannelType   string         `json:"channel_type"`
	Config        map[string]any `json:"config"`
	SigningSecret string         `json:"signing_secret"`
	IsActive      *bool          `json:"is_active"`
}

type notificationChannelPatch struct {
	Name          string         `json:"name"`
	Config        map[string]any `json:"config"`
	IsActive      *bool          `json:"is_active"`
	SigningSecret string         `json:"signing_secret"`
}

func parseChannelPayload(p notificationChannelPayload) (name, channelType string, err error) {
	name = strings.TrimSpace(p.Name)
	if name == "" {
		name = "Notification channel"
	}
	channelType = strings.ToLower(strings.TrimSpace(p.ChannelType))
	if channelType == "" {
		channelType = strings.ToLower(strings.TrimSpace(p.Type))
	}
	switch channelType {
	case notifications.TypeSMTP, notifications.TypeWebhook, notifications.TypeSlackIncoming:
		return name, channelType, nil
	default:
		return "", "", errors.New("channel_type must be smtp, webhook, or slack")
	}
}

func maskChannelPublicConfig(typ string, cfg map[string]any) map[string]any {
	out := cfg
	if out == nil {
		out = map[string]any{}
	}
	masked := make(map[string]any, len(out))
	for k, v := range out {
		masked[k] = v
	}
	if strings.EqualFold(typ, notifications.TypeSMTP) {
		if _, ok := masked["password"]; ok {
			masked["password"] = ""
		}
	}
	return masked
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

func mergeSigningSecretPreserve(existing, patch string) string {
	p := strings.TrimSpace(patch)
	if p == "" {
		return strings.TrimSpace(existing)
	}
	return p
}

// RegisterAlertRoutes wires outbound channels, alerting rules, and active alert timelines.
func RegisterAlertRoutes(mux *http.ServeMux, d AlertsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil || d.Dispatcher == nil {
		panic("api: alerts routes require Pool, Logger, Auth.JWT, and Dispatcher")
	}
	h := &alertsMux{deps: d}

	dup := func(method, canonical, compat string, fn func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(method+" "+canonical, fn)
		if compat != "" {
			mux.HandleFunc(method+" "+compat, fn)
		}
	}

	dup("GET", "/v1/alerts/channels", "/v1/alerts/settings", h.handleListChannels)
	dup("POST", "/v1/alerts/channels", "/v1/alerts/settings", h.handleCreateChannel)
	dup("PATCH", "/v1/alerts/channels/{id}", "/v1/alerts/settings/{id}", h.handlePatchChannel)
	dup("POST", "/v1/alerts/channels/{id}/test", "/v1/alerts/settings/{id}/test", h.handleTestChannel)

	mux.HandleFunc("GET /v1/alerts/rules", h.handleListRules)
	mux.HandleFunc("POST /v1/alerts/rules", h.handleCreateRule)
	mux.HandleFunc("PATCH /v1/alerts/rules/{id}", h.handlePatchRule)
	mux.HandleFunc("DELETE /v1/alerts/rules/{id}", h.handleDeleteRule)

	mux.HandleFunc("GET /v1/alerts/active", h.handleListActiveAlerts)
	mux.HandleFunc("POST /v1/alerts/active/{id}/resolve", h.handleResolveActiveAlert)
}

type alertsMux struct {
	deps AlertsDeps
}

func (h *alertsMux) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin)
	return ok
}

func (h *alertsMux) handleListChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, COALESCE(trim(name),''),
       channel_type, config_json::text,
       CASE WHEN signing_secret <> '' THEN true ELSE false END,
       is_active, created_at, updated_at
FROM notification_channels ORDER BY channel_type ASC, created_at ASC
`)
	if err != nil {
		h.deps.Logger.Error("notifications: list notification_channels", "err", err)
		writeAlertsErr(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	defer rows.Close()

	var out []notificationChannelDTO
	for rows.Next() {
		var dto notificationChannelDTO
		var cfgText string
		if err := rows.Scan(&dto.ID, &dto.Name, &dto.ChannelType, &cfgText,
			&dto.SigningSecretConfigured, &dto.IsActive, &dto.CreatedAt, &dto.UpdatedAt); err != nil {
			writeAlertsErr(w, http.StatusInternalServerError, "failed to scan channel")
			return
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(cfgText), &cfg); err != nil {
			cfg = map[string]any{}
		}
		dto.Config = maskChannelPublicConfig(dto.ChannelType, cfg)
		out = append(out, dto)
	}
	if err := rows.Err(); err != nil {
		writeAlertsErr(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	if out == nil {
		out = []notificationChannelDTO{}
	}
	writeAlertsPayload(w, http.StatusOK, out)
}

func (h *alertsMux) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "could not read body")
		return
	}
	var req notificationChannelPayload
	if err := json.Unmarshal(body, &req); err != nil || req.Config == nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name, ctype, err := parseChannelPayload(req)
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, err.Error())
		return
	}

	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	cfgJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid config")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var newID uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO notification_channels (name, channel_type, config_json, signing_secret, is_active)
VALUES ($1,$2,$3::jsonb,$4,$5) RETURNING id
`, name, ctype, string(cfgJSON), strings.TrimSpace(req.SigningSecret), active).Scan(&newID)
	if err != nil {
		h.deps.Logger.Error("notifications: insert channel", "err", err)
		writeAlertsErr(w, http.StatusInternalServerError, "failed to save channel")
		return
	}
	writeAlertsPayload(w, http.StatusCreated, map[string]string{"id": newID.String()})
}

func (h *alertsMux) handlePatchChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "could not read body")
		return
	}
	var req notificationChannelPatch
	if err := json.Unmarshal(body, &req); err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Config == nil && req.IsActive == nil && strings.TrimSpace(req.SigningSecret) == "" &&
		strings.TrimSpace(req.Name) == "" {
		writeAlertsErr(w, http.StatusBadRequest, "no fields supplied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var ctype string
	var curJSON []byte
	var secret string
	var curActive bool
	var curName string
	err = h.deps.Pool.QueryRow(ctx, `
SELECT channel_type, config_json::text, COALESCE(signing_secret,''), is_active,
       COALESCE(trim(name),'')
FROM notification_channels WHERE id = $1
`, id).Scan(&ctype, &curJSON, &secret, &curActive, &curName)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAlertsErr(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		writeAlertsErr(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	nextNameStr := strings.TrimSpace(curName)
	if v := strings.TrimSpace(req.Name); v != "" {
		nextNameStr = v
	}

	var nextCfg map[string]any
	if err := json.Unmarshal(curJSON, &nextCfg); err != nil || nextCfg == nil {
		nextCfg = map[string]any{}
	}
	if req.Config != nil {
		if ctype == notifications.TypeSMTP {
			nextCfg = mergeSMTPPasswordPreserve(nextCfg, req.Config)
		} else {
			nextCfg = req.Config
		}
	}

	nextSecret := mergeSigningSecretPreserve(secret, req.SigningSecret)
	nextActive := curActive
	if req.IsActive != nil {
		nextActive = *req.IsActive
	}

	cfgMarshaled, err := json.Marshal(nextCfg)
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid config")
		return
	}

	_, err = h.deps.Pool.Exec(ctx, `
UPDATE notification_channels
SET name = $2, config_json = $3::jsonb, signing_secret = $4, is_active = $5, updated_at = now()
WHERE id = $1
`, id, curNameOrDefault(nextNameStr), string(cfgMarshaled), nextSecret, nextActive)
	if err != nil {
		h.deps.Logger.Error("notifications: update channel", "err", err)
		writeAlertsErr(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeAlertsPayload(w, http.StatusOK, map[string]string{"status": "updated"})
}

func curNameOrDefault(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Notification channel"
	}
	return strings.TrimSpace(name)
}

func (h *alertsMux) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := h.deps.Dispatcher.SendTestChannel(ctx, id); err != nil {
		h.deps.Logger.Warn("notifications: test dispatch failed", "err", err, "channel_id", id.String())
		writeAlertsErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAlertsPayload(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (h *alertsMux) handleListRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rules, err := database.ListAlertRules(ctx, h.deps.Pool)
	if err != nil {
		writeAlertsErr(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeAlertsPayload(w, http.StatusOK, rules)
}

func (h *alertsMux) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "could not read body")
		return
	}
	var tmp map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tmp); err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	var rr models.AlertRule
	if err := json.Unmarshal(raw, &rr); err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid rule json")
		return
	}
	if v, ok := tmp["threshold"]; ok {
		var f float64
		if json.Unmarshal(v, &f) == nil {
			rr.Threshold = f
		}
	}
	if td, ok := tmp["target_device_id"]; ok && len(td) > 4 {
		var s string
		if json.Unmarshal(td, &s) == nil {
			ts := strings.TrimSpace(s)
			if ts != "" {
				idu, err := uuid.Parse(ts)
				if err != nil {
					writeAlertsErr(w, http.StatusBadRequest, "invalid target_device_id")
					return
				}
				if idu != uuid.Nil {
					rr.TargetDeviceID = &idu
				}
			}
		}
	}

	if rr.DurationSeconds <= 0 {
		writeAlertsErr(w, http.StatusBadRequest, "duration_seconds must be > 0")
		return
	}
	isEnabled := true
	if _, ok := tmp["is_enabled"]; ok {
		_ = json.Unmarshal(tmp["is_enabled"], &isEnabled)
	}
	rr.IsEnabled = isEnabled

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	id, err := database.InsertAlertRule(ctx, h.deps.Pool, rr)
	if err != nil {
		h.deps.Logger.Error("alert_rules: insert", "err", err)
		writeAlertsErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAlertsPayload(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *alertsMux) handlePatchRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAlertJSONBody))
	_ = r.Body.Close()
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "could not read body")
		return
	}
	var tmp map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tmp); err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(tmp) == 0 {
		writeAlertsErr(w, http.StatusBadRequest, "no mutable fields supplied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	base, err := database.LoadAlertRule(ctx, h.deps.Pool, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAlertsErr(w, http.StatusNotFound, "rule not found")
			return
		}
		writeAlertsErr(w, http.StatusInternalServerError, "failed to load rule")
		return
	}

	var patch models.AlertRule
	if err := json.Unmarshal(raw, &patch); err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid rule json")
		return
	}

	merged := base
	if _, ok := tmp["name"]; ok {
		merged.Name = strings.TrimSpace(patch.Name)
	}
	if _, ok := tmp["target_type"]; ok {
		merged.TargetType = strings.TrimSpace(patch.TargetType)
	}
	if _, ok := tmp["metric"]; ok {
		merged.Metric = strings.TrimSpace(patch.Metric)
	}
	if _, ok := tmp["operator"]; ok {
		merged.Operator = strings.TrimSpace(patch.Operator)
	}
	if v, ok := tmp["threshold"]; ok {
		var f float64
		if json.Unmarshal(v, &f) != nil {
			writeAlertsErr(w, http.StatusBadRequest, "invalid threshold")
			return
		}
		merged.Threshold = f
	}
	if _, ok := tmp["duration_seconds"]; ok {
		if patch.DurationSeconds <= 0 {
			writeAlertsErr(w, http.StatusBadRequest, "duration_seconds must be positive")
			return
		}
		merged.DurationSeconds = patch.DurationSeconds
	}
	if _, ok := tmp["severity"]; ok {
		merged.Severity = strings.TrimSpace(patch.Severity)
	}
	if _, ok := tmp["is_enabled"]; ok {
		_ = json.Unmarshal(tmp["is_enabled"], &merged.IsEnabled)
	}
	if v, ok := tmp["target_device_id"]; ok {
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			writeAlertsErr(w, http.StatusBadRequest, "invalid target_device_id")
			return
		}
		switch t := decoded.(type) {
		case nil:
			merged.TargetDeviceID = nil
		case string:
			sid := strings.TrimSpace(t)
			if sid == "" {
				merged.TargetDeviceID = nil
			} else {
				idu, err := uuid.Parse(sid)
				if err != nil {
					writeAlertsErr(w, http.StatusBadRequest, "invalid target_device_id")
					return
				}
				merged.TargetDeviceID = &idu
			}
		default:
			writeAlertsErr(w, http.StatusBadRequest, "target_device_id must be a string UUID or null")
			return
		}
	}
	merged.ID = id

	if err := database.UpdateAlertRule(ctx, h.deps.Pool, id, merged); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAlertsErr(w, http.StatusNotFound, "rule not found")
			return
		}
		writeAlertsErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAlertsPayload(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *alertsMux) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := database.DeleteAlertRule(ctx, h.deps.Pool, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAlertsErr(w, http.StatusNotFound, "rule not found")
			return
		}
		writeAlertsErr(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type activeAlertDTO struct {
	ID             uuid.UUID       `json:"id"`
	AlertRuleID    *uuid.UUID      `json:"alert_rule_id,omitempty"`
	Fingerprint    string          `json:"fingerprint"`
	AlertKind      string          `json:"alert_kind"`
	DeviceID       *uuid.UUID      `json:"device_id,omitempty"`
	Severity       string          `json:"severity"`
	Title          string          `json:"title"`
	Message        string          `json:"message"`
	Details        json.RawMessage `json:"details"`
	TriggeredAt    time.Time       `json:"triggered_at"`
	LastNotifiedAt *time.Time      `json:"last_notified_at,omitempty"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
}

func (h *alertsMux) handleListActiveAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	inc := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_resolved")), "true") ||
		r.URL.Query().Get("include_resolved") == "1"

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rows, err := database.ListActiveAlerts(ctx, h.deps.Pool, limit, inc)
	if err != nil {
		writeAlertsErr(w, http.StatusInternalServerError, "failed to list alerts")
		return
	}
	out := make([]activeAlertDTO, 0, len(rows))
	for _, a := range rows {
		raw := json.RawMessage(a.Details)
		if len(a.Details) == 0 {
			raw = json.RawMessage(`{}`)
		} else if !json.Valid(a.Details) {
			raw = json.RawMessage(`{}`)
		}
		out = append(out, activeAlertDTO{
			ID:             a.ID,
			AlertRuleID:    a.AlertRuleID,
			Fingerprint:    a.Fingerprint,
			AlertKind:      a.AlertKind,
			DeviceID:       a.DeviceID,
			Severity:       a.Severity,
			Title:          a.Title,
			Message:        a.Message,
			Details:        raw,
			TriggeredAt:    a.TriggeredAt,
			LastNotifiedAt: a.LastNotifiedAt,
			ResolvedAt:     a.ResolvedAt,
		})
	}
	writeAlertsPayload(w, http.StatusOK, out)
}

func (h *alertsMux) handleResolveActiveAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAlertsErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeAlertsErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := database.ResolveActiveAlertByID(ctx, h.deps.Pool, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAlertsErr(w, http.StatusNotFound, "alert not found")
			return
		}
		writeAlertsErr(w, http.StatusInternalServerError, "failed to resolve alert")
		return
	}
	writeAlertsPayload(w, http.StatusOK, map[string]string{"status": "resolved"})
}
