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
	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxDeviceCommandPayloadBytes = 64 * 1024

// DeviceCommandsDeps wires REST device command routes and C2 dispatch.
type DeviceCommandsDeps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	Auth         DashboardAuth
	Dispatch     func(certSerial string, payload any) bool
	ResolveAsset func(ctx context.Context, deviceID uuid.UUID) (certSerial string, err error)
}

type deviceCommandsHandler struct {
	deps DeviceCommandsDeps
}

// RegisterDeviceCommandRoutes registers /v1/devices/{id}/commands (dashboard auth).
func RegisterDeviceCommandRoutes(mux *http.ServeMux, d DeviceCommandsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: device commands handler requires Pool, Logger, and Auth.JWT")
	}
	if d.Dispatch == nil || d.ResolveAsset == nil {
		panic("api: device commands handler requires Dispatch and ResolveAsset")
	}
	h := &deviceCommandsHandler{deps: d}
	mux.HandleFunc("GET /v1/devices/{id}/commands", h.handleList)
	mux.HandleFunc("POST /v1/devices/{id}/commands", h.handleCreate)
}

func (h *deviceCommandsHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *deviceCommandsHandler) parseDeviceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return uuid.Nil, false
	}
	return id, true
}

func (h *deviceCommandsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	deviceID, ok := h.parseDeviceID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.ensureAssetExists(ctx, deviceID); err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}
	commands, err := database.ListDeviceCommands(ctx, h.deps.Pool, deviceID, 100)
	if err != nil {
		h.deps.Logger.Error("list device commands failed", "err", err, "device_id", deviceID)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list commands")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]any{"commands": commands})
}

type createDeviceCommandBody struct {
	CommandType string `json:"command_type"`
	Payload     string `json:"payload"`
}

func (h *deviceCommandsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	principal, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	deviceID, ok := h.parseDeviceID(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxDeviceCommandPayloadBytes+4096))
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req createDeviceCommandBody
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	commandType := strings.TrimSpace(strings.ToLower(req.CommandType))
	if commandType == "" {
		writeTicketsError(w, http.StatusBadRequest, "command_type is required")
		return
	}
	payload := req.Payload
	switch commandType {
	case models.DeviceCommandTypeScript:
		if !utf8.ValidString(payload) {
			writeTicketsError(w, http.StatusBadRequest, "script payload must be valid UTF-8")
			return
		}
		if len(payload) > maxDeviceCommandPayloadBytes {
			writeTicketsError(w, http.StatusBadRequest, fmt.Sprintf("script payload exceeds %d bytes", maxDeviceCommandPayloadBytes))
			return
		}
		if strings.TrimSpace(payload) == "" {
			writeTicketsError(w, http.StatusBadRequest, "script payload is required")
			return
		}
	case models.DeviceCommandTypeRestartService:
		payload = strings.TrimSpace(payload)
		if payload == "" {
			writeTicketsError(w, http.StatusBadRequest, "restart_service requires service name payload")
			return
		}
		if len(payload) > 256 {
			writeTicketsError(w, http.StatusBadRequest, "restart_service payload too long")
			return
		}
	case models.DeviceCommandTypePushConfig:
		if strings.TrimSpace(payload) == "" {
			writeTicketsError(w, http.StatusBadRequest, "push_config requires JSON payload")
			return
		}
		if len(payload) > maxDeviceCommandPayloadBytes {
			writeTicketsError(w, http.StatusBadRequest, fmt.Sprintf("push_config payload exceeds %d bytes", maxDeviceCommandPayloadBytes))
			return
		}
		var probe map[string]any
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			writeTicketsError(w, http.StatusBadRequest, "push_config payload must be JSON")
			return
		}
	case models.DeviceCommandTypeQuarantine:
		if strings.TrimSpace(payload) == "" {
			writeTicketsError(w, http.StatusBadRequest, "quarantine requires JSON payload")
			return
		}
		if len(payload) > maxDeviceCommandPayloadBytes {
			writeTicketsError(w, http.StatusBadRequest, fmt.Sprintf("quarantine payload exceeds %d bytes", maxDeviceCommandPayloadBytes))
			return
		}
		var probe map[string]any
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			writeTicketsError(w, http.StatusBadRequest, "quarantine payload must be JSON")
			return
		}
	case models.DeviceCommandTypePing, models.DeviceCommandTypeReboot:
		if strings.TrimSpace(payload) != "" {
			writeTicketsError(w, http.StatusBadRequest, "payload is not used for ping or reboot commands")
			return
		}
	default:
		writeTicketsError(w, http.StatusBadRequest, "unsupported command_type")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.ensureAssetExists(ctx, deviceID); err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}

	cmd, err := database.InsertDeviceCommand(ctx, h.deps.Pool, deviceID, commandType, payload, nil)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	certSerial, err := h.deps.ResolveAsset(ctx, deviceID)
	if err != nil {
		_ = database.FailDeviceCommandIfPending(ctx, h.deps.Pool, cmd.ID, "asset certificate not available: "+err.Error())
		writeTicketsError(w, http.StatusConflict, err.Error())
		return
	}

	downlink := map[string]string{
		"action":       "device_command",
		"command_id":   cmd.ID.String(),
		"command_type": commandType,
		"payload":      payload,
	}
	if !h.deps.Dispatch(certSerial, downlink) {
		failCtx, failCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer failCancel()
		_, _ = database.FailDeviceCommand(failCtx, h.deps.Pool, cmd.ID, "agent not connected")
		writeTicketsError(w, http.StatusConflict, "agent not connected")
		return
	}

	if err := database.MarkDeviceCommandSent(ctx, h.deps.Pool, cmd.ID); err != nil {
		h.deps.Logger.Warn("mark device command sent failed", "err", err, "command_id", cmd.ID)
	}
	cmd.Status = models.DeviceCommandStatusSent

	auditCtx, auditCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer auditCancel()
	_ = auth.InsertAuditRecord(auditCtx, h.deps.Pool, auth.AuditRecord{
		UserID:        principal.UserID,
		Action:        "command_executed",
		ResourceType:  "device",
		ResourceID:    &deviceID,
		TargetAssetID: &deviceID,
		Details: map[string]any{
			"command_id":   cmd.ID.String(),
			"command_type": commandType,
			"channel":      "rest",
		},
		IPAddress: auth.ClientIP(r),
	})

	writeTicketsJSON(w, http.StatusCreated, cmd)
}

func (h *deviceCommandsHandler) ensureAssetExists(ctx context.Context, deviceID uuid.UUID) error {
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

// ResolveAssetCertSerial loads cert_serial for a device (asset) UUID.
func ResolveAssetCertSerial(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID) (string, error) {
	if pool == nil {
		return "", errors.New("pool is required")
	}
	var cs *string
	err := pool.QueryRow(ctx, `SELECT cert_serial FROM assets WHERE id = $1 LIMIT 1`, deviceID).Scan(&cs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("device not found")
		}
		return "", fmt.Errorf("device lookup failed")
	}
	if cs == nil || strings.TrimSpace(*cs) == "" {
		return "", fmt.Errorf("device has no enrolled certificate")
	}
	return strings.TrimSpace(*cs), nil
}
