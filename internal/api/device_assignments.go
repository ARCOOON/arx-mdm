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

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAssignmentJSONBody = 8 << 10

// DeviceAssignmentsDeps wires device custodian REST APIs.
type DeviceAssignmentsDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type deviceAssignmentsHandler struct {
	deps DeviceAssignmentsDeps
}

type assignDeviceBody struct {
	UserID string `json:"user_id"`
}

// DeviceAssignmentWire is the current custodian snapshot for GET /v1/devices/{id}/assignment.
type DeviceAssignmentWire struct {
	UserID     uuid.UUID `json:"user_id"`
	Username   string    `json:"username"`
	AssignedAt time.Time `json:"assigned_at"`
}

// RegisterDeviceAssignmentRoutes registers POST assign/unassign and GET current assignment for a device UUID.
func RegisterDeviceAssignmentRoutes(mux *http.ServeMux, d DeviceAssignmentsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: device assignment routes require Pool, Logger, and Auth.JWT")
	}
	h := &deviceAssignmentsHandler{deps: d}
	mux.HandleFunc("GET /v1/devices/{id}/assignment", h.handleGetAssignment)
	mux.HandleFunc("POST /v1/devices/{id}/assign", h.handleAssign)
	mux.HandleFunc("POST /v1/devices/{id}/unassign", h.handleUnassign)
}

func (h *deviceAssignmentsHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func parseDeviceAssignmentUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *deviceAssignmentsHandler) ensureAsset(ctx context.Context, deviceID uuid.UUID) error {
	var exists bool
	err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id = $1)`, deviceID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("device lookup failed: %w", err)
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

func (h *deviceAssignmentsHandler) handleGetAssignment(w http.ResponseWriter, r *http.Request) {
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

	if err := h.ensureAsset(ctx, deviceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "device not found")
			return
		}
		h.deps.Logger.Error("assignment device lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to load assignment")
		return
	}

	row := h.deps.Pool.QueryRow(ctx, `
SELECT d.user_id, u.username, d.assigned_at
FROM device_assignments d
JOIN users u ON u.id = d.user_id
WHERE d.asset_id = $1
`, deviceID)
	var wire DeviceAssignmentWire
	err := row.Scan(&wire.UserID, &wire.Username, &wire.AssignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsJSON(w, http.StatusOK, map[string]any{"assignment": nil})
		return
	}
	if err != nil {
		h.deps.Logger.Error("read assignment", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to load assignment")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]any{"assignment": wire})
}

func (h *deviceAssignmentsHandler) handleAssign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	principal, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	deviceID, ok := parseDeviceAssignmentUUID(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAssignmentJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var payload assignDeviceBody
	if err := json.Unmarshal(body, &payload); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	userID, err := uuid.Parse(strings.TrimSpace(payload.UserID))
	if err != nil || userID == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "user_id must be a non-empty UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := h.ensureAsset(ctx, deviceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "device not found")
			return
		}
		h.deps.Logger.Error("assign device lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}

	tx, err := h.deps.Pool.Begin(ctx)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existsUser bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&existsUser); err != nil {
		h.deps.Logger.Error("assign user lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}
	if !existsUser {
		writeTicketsError(w, http.StatusBadRequest, "user_id not found")
		return
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO device_assignments (asset_id, user_id, assigned_at)
VALUES ($1, $2, now())
ON CONFLICT (asset_id) DO UPDATE SET user_id = EXCLUDED.user_id, assigned_at = now()
`, deviceID, userID); err != nil {
		h.deps.Logger.Error("assign upsert", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}
	var wire DeviceAssignmentWire
	if err := tx.QueryRow(ctx, `
SELECT d.user_id, u.username, d.assigned_at
FROM device_assignments d
JOIN users u ON u.id = d.user_id
WHERE d.asset_id = $1
`, deviceID).Scan(&wire.UserID, &wire.Username, &wire.AssignedAt); err != nil {
		h.deps.Logger.Error("assign read-back", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to assign device")
		return
	}

	details := map[string]any{
		"assigned_user_id":  wire.UserID.String(),
		"assigned_username": wire.Username,
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	_ = auth.InsertAuditRecord(auditCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        "device_assigned",
		ResourceType:  "device",
		ResourceID:    &deviceID,
		TargetAssetID: &deviceID,
		Details:       details,
		IPAddress:     auth.ClientIP(r),
	})

	writeTicketsJSON(w, http.StatusOK, map[string]any{"assignment": wire})
}

func (h *deviceAssignmentsHandler) handleUnassign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	principal, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	deviceID, ok := parseDeviceAssignmentUUID(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := h.ensureAsset(ctx, deviceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusNotFound, "device not found")
			return
		}
		h.deps.Logger.Error("unassign device lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to unassign device")
		return
	}

	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM device_assignments WHERE asset_id = $1`, deviceID)
	if err != nil {
		h.deps.Logger.Error("unassign delete", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to unassign device")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsJSON(w, http.StatusOK, map[string]any{"assignment": nil, "status": "already_unassigned"})
		return
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	_ = auth.InsertAuditRecord(auditCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        "device_unassigned",
		ResourceType:  "device",
		ResourceID:    &deviceID,
		TargetAssetID: &deviceID,
		Details:       map[string]any{},
		IPAddress:     auth.ClientIP(r),
	})
	writeTicketsJSON(w, http.StatusOK, map[string]any{"assignment": nil})
}
