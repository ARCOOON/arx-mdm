package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/mdm/compliance"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantComplianceDeps wires GET/PATCH /v1/tenant/compliance-settings.
type TenantComplianceDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type tenantComplianceHandler struct {
	deps TenantComplianceDeps
}

// RegisterTenantComplianceRoutes registers tenant-wide compliance toggles (dashboard JWT).
func RegisterTenantComplianceRoutes(mux *http.ServeMux, d TenantComplianceDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: tenant compliance routes require Pool, Logger, Auth.JWT")
	}
	h := &tenantComplianceHandler{deps: d}
	mux.HandleFunc("GET /v1/tenant/compliance-settings", h.handleGet)
	mux.HandleFunc("PATCH /v1/tenant/compliance-settings", h.handlePatch)
}

func (h *tenantComplianceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	on, err := database.TenantAutoQuarantine(ctx, h.deps.Pool)
	if err != nil {
		h.deps.Logger.Error("tenant compliance settings read failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load tenant settings")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]any{
		"auto_quarantine_on_noncompliance": on,
	})
}

type patchTenantComplianceBody struct {
	AutoQuarantineOnNonCompliance *bool `json:"auto_quarantine_on_noncompliance"`
}

func (h *tenantComplianceHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req patchTenantComplianceBody
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.AutoQuarantineOnNonCompliance == nil {
		writeTicketsError(w, http.StatusBadRequest, "auto_quarantine_on_noncompliance is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := database.SetTenantAutoQuarantine(ctx, h.deps.Pool, *req.AutoQuarantineOnNonCompliance); err != nil {
		h.deps.Logger.Error("tenant compliance settings update failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to update tenant settings")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]any{
		"auto_quarantine_on_noncompliance": *req.AutoQuarantineOnNonCompliance,
	})
}

// DeviceQuarantineDeps wires PATCH /v1/devices/{id}/quarantine.
type DeviceQuarantineDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	Auth         DashboardAuth
	Dispatch     func(certSerial string, payload any) bool
	ResolveAsset func(ctx context.Context, deviceID uuid.UUID) (certSerial string, err error)
}

type deviceQuarantineHandler struct {
	deps DeviceQuarantineDeps
}

type certSerialDispatcher func(certSerial string, payload any) bool

func (f certSerialDispatcher) DispatchJSON(certSerial string, payload any) bool {
	if f == nil {
		return false
	}
	return f(certSerial, payload)
}

// RegisterDeviceQuarantineRoutes registers manual isolation toggle for one asset.
func RegisterDeviceQuarantineRoutes(mux *http.ServeMux, d DeviceQuarantineDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: device quarantine routes require Pool, Logger, Auth.JWT")
	}
	if d.Dispatch == nil || d.ResolveAsset == nil {
		panic("api: device quarantine routes require Dispatch and ResolveAsset")
	}
	h := &deviceQuarantineHandler{deps: d}
	mux.HandleFunc("PATCH /v1/devices/{id}/quarantine", h.handlePatch)
}

type patchDeviceQuarantineBody struct {
	QuarantineEnabled bool `json:"quarantine_enabled"`
}

func (h *deviceQuarantineHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	raw := strings.TrimSpace(r.PathValue("id"))
	deviceID, err := uuid.Parse(raw)
	if err != nil || deviceID == uuid.Nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req patchDeviceQuarantineBody
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var exists bool
	if err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id = $1)`, deviceID).Scan(&exists); err != nil || !exists {
		writeTicketsError(w, http.StatusNotFound, "device not found")
		return
	}

	if err := database.SetAssetQuarantineEnabled(ctx, h.deps.Pool, deviceID, req.QuarantineEnabled); err != nil {
		h.deps.Logger.Error("device quarantine flag update failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to update device")
		return
	}

	certSerial, err := h.deps.ResolveAsset(ctx, deviceID)
	if err != nil {
		writeTicketsError(w, http.StatusConflict, err.Error())
		return
	}

	cmd, dispatched, qerr := compliance.QueueQuarantineCommand(ctx, h.deps.Pool, certSerialDispatcher(h.deps.Dispatch), deviceID, certSerial, req.QuarantineEnabled)
	if qerr != nil {
		h.deps.Logger.Error("device quarantine command enqueue failed", "err", qerr)
		writeTicketsError(w, http.StatusBadRequest, qerr.Error())
		return
	}

	writeTicketsJSON(w, http.StatusOK, map[string]any{
		"quarantine_enabled": req.QuarantineEnabled,
		"command_id":         cmd.ID.String(),
		"dispatched":         dispatched,
	})
}
