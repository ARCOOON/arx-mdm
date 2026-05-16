package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeviceMetricsDeps wires GET /v1/devices/{id}/metrics.
type DeviceMetricsDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type deviceMetricsHandler struct {
	deps DeviceMetricsDeps
}

// RegisterDeviceMetricsRoutes registers GET /v1/devices/{id}/metrics (dashboard auth).
func RegisterDeviceMetricsRoutes(mux *http.ServeMux, d DeviceMetricsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: device metrics handler requires Pool, Logger, and Auth.JWT")
	}
	h := &deviceMetricsHandler{deps: d}
	mux.HandleFunc("GET /v1/devices/{id}/metrics", h.handleGet)
}

func (h *deviceMetricsHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *deviceMetricsHandler) parseDeviceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid device id")
		return uuid.Nil, false
	}
	return id, true
}

func parseMetricsHours(r *http.Request) int {
	q := strings.TrimSpace(r.URL.Query().Get("hours"))
	if q == "" {
		return 24
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return 24
	}
	return n
}

func (h *deviceMetricsHandler) ensureAssetExists(ctx context.Context, deviceID uuid.UUID) error {
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

func (h *deviceMetricsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
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
	hours := parseMetricsHours(r)

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := h.ensureAssetExists(ctx, deviceID); err != nil {
		writeTicketsError(w, http.StatusNotFound, err.Error())
		return
	}

	history, err := database.LoadDeviceMetricHistory(ctx, h.deps.Pool, deviceID, hours)
	if err != nil {
		h.deps.Logger.Error("load device metrics failed", "err", err, "device_id", deviceID)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load metrics")
		return
	}
	if history.Series == nil {
		history.Series = []database.DeviceMetricSeriesPoint{}
	}
	writeTicketsJSON(w, http.StatusOK, history)
}
