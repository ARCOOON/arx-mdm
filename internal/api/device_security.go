package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceSecurityDeps wires admin-only remote lock and enterprise wipe REST routes.
type DeviceSecurityDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	Auth         DashboardAuth
	Dispatch     func(certSerial string, payload any) bool
	ResolveAsset func(ctx context.Context, deviceID uuid.UUID) (certSerial string, err error)
}

type deviceSecurityHandler struct {
	deps DeviceSecurityDeps
}

// RegisterDeviceSecurityRoutes registers POST /v1/devices/{id}/lock and /wipe (admin only).
func RegisterDeviceSecurityRoutes(mux *http.ServeMux, d DeviceSecurityDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: device security requires Pool, Logger, and Auth.JWT")
	}
	if d.Dispatch == nil || d.ResolveAsset == nil {
		panic("api: device security requires Dispatch and ResolveAsset")
	}
	h := &deviceSecurityHandler{deps: d}
	mux.HandleFunc("POST /v1/devices/{id}/lock", h.handleLock)
	mux.HandleFunc("POST /v1/devices/{id}/wipe", h.handleWipe)
}

func (h *deviceSecurityHandler) parseDeviceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *deviceSecurityHandler) handleLock(w http.ResponseWriter, r *http.Request) {
	h.dispatchSecurityAction(w, r, "lock", "device_remote_lock")
}

func (h *deviceSecurityHandler) handleWipe(w http.ResponseWriter, r *http.Request) {
	h.dispatchSecurityAction(w, r, "wipe", "device_enterprise_wipe")
}

func (h *deviceSecurityHandler) dispatchSecurityAction(w http.ResponseWriter, r *http.Request, action, auditAction string) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	principal, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin)
	if !ok {
		return
	}

	deviceID, ok := h.parseDeviceID(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	ip := auth.ClientIP(r)

	auditPreCtx, auditPreCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = auth.InsertAuditRecord(auditPreCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        auditAction + "_request",
		ResourceType:  "device",
		ResourceID:    &deviceID,
		TargetAssetID: &deviceID,
		Details: map[string]any{
			"transport_action": action,
			"channel":          "rest",
			"phase":            "request_received",
		},
		IPAddress: ip,
	})
	auditPreCancel()

	if err := h.ensureAssetExists(ctx, deviceID); err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}

	certSerial, err := h.deps.ResolveAsset(ctx, deviceID)
	if err != nil {
		writeTicketsError(w, http.StatusConflict, err.Error())
		return
	}

	payload := map[string]string{"action": action}
	if !h.deps.Dispatch(certSerial, payload) {
		auditFailCtx, auditFailCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = auth.InsertAuditRecord(auditFailCtx, h.deps.Pool, auth.AuditRecord{
			UserID:        principal.UserID,
			Action:        auditAction + "_dispatch_failed",
			ResourceType:  "device",
			ResourceID:    &deviceID,
			TargetAssetID: &deviceID,
			Details: map[string]any{
				"transport_action": action,
				"channel":          "rest",
				"reason":           "agent_not_connected",
			},
			IPAddress: ip,
		})
		auditFailCancel()
		writeTicketsError(w, http.StatusConflict, "agent not connected")
		return
	}

	okDetails := map[string]any{
		"transport_action": action,
		"channel":          "rest",
		"phase":            "c2_dispatch_succeeded",
	}
	auditOkCtx, auditOkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = auth.InsertAuditRecord(auditOkCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        auditAction + "_dispatched",
		ResourceType:  "device",
		ResourceID:    &deviceID,
		TargetAssetID: &deviceID,
		Details:       okDetails,
		IPAddress:     ip,
	})
	auditOkCancel()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"device_id":  deviceID.String(),
		"action":     action,
		"dispatched": true,
	})
}

func (h *deviceSecurityHandler) ensureAssetExists(ctx context.Context, deviceID uuid.UUID) error {
	var exists bool
	err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id = $1)`, deviceID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("device lookup failed")
	}
	if !exists {
		return fmt.Errorf("device not found")
	}
	return nil
}
