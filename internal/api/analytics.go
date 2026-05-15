package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

const analyticsOnlineThresholdSeconds int64 = 300

// AnalyticsDeps wires GET /v1/analytics/summary.
type AnalyticsDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type analyticsJSONError struct {
	Error string `json:"error"`
}

func writeAnalyticsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAnalyticsError(w http.ResponseWriter, status int, msg string) {
	writeAnalyticsJSON(w, status, analyticsJSONError{Error: msg})
}

// AnalyticsSummaryResponse is returned by GET /v1/analytics/summary.
type AnalyticsSummaryResponse struct {
	OnlineThresholdSeconds int64 `json:"online_threshold_seconds"`
	Assets                 struct {
		Total   int64 `json:"total"`
		Online  int64 `json:"online"`
		Offline int64 `json:"offline"`
	} `json:"assets"`
	OSDistribution map[string]int64 `json:"os_distribution"`
	Tickets        struct {
		Unresolved int64 `json:"unresolved"`
	} `json:"tickets"`
}

// RegisterAnalyticsRoutes registers dashboard analytics read APIs.
func RegisterAnalyticsRoutes(mux *http.ServeMux, d AnalyticsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: analytics routes require Pool, Logger, and Auth.JWT")
	}
	h := &analyticsHandler{deps: d}
	mux.HandleFunc("GET /v1/analytics/summary", h.handleSummary)
}

type analyticsHandler struct {
	deps AnalyticsDeps
}

func (h *analyticsHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAnalyticsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var total, online, unresolved int64
	err := h.deps.Pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*)::bigint FROM assets) AS total,
  (SELECT COUNT(*)::bigint FROM assets
   WHERE last_seen IS NOT NULL AND last_seen >= now() - ($1::bigint * interval '1 second')) AS online,
  (SELECT COUNT(*)::bigint FROM tickets
   WHERE lower(status) NOT IN ('resolved', 'closed')) AS unresolved
`, analyticsOnlineThresholdSeconds).Scan(&total, &online, &unresolved)
	if err != nil {
		h.deps.Logger.Error("analytics summary aggregate", "err", err)
		writeAnalyticsError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}

	rows, err := h.deps.Pool.Query(ctx, `
SELECT os_type, COUNT(*)::bigint
FROM assets
GROUP BY os_type
`)
	if err != nil {
		h.deps.Logger.Error("analytics os distribution", "err", err)
		writeAnalyticsError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	defer rows.Close()
	osDist := make(map[string]int64)
	for rows.Next() {
		var os string
		var c int64
		if err := rows.Scan(&os, &c); err != nil {
			h.deps.Logger.Error("analytics os row scan", "err", err)
			writeAnalyticsError(w, http.StatusInternalServerError, "failed to load analytics")
			return
		}
		osDist[os] = c
	}
	if err := rows.Err(); err != nil {
		h.deps.Logger.Error("analytics os rows", "err", err)
		writeAnalyticsError(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}

	var out AnalyticsSummaryResponse
	out.OnlineThresholdSeconds = analyticsOnlineThresholdSeconds
	out.Assets.Total = total
	out.Assets.Online = online
	if total >= online {
		out.Assets.Offline = total - online
	}
	out.OSDistribution = osDist
	out.Tickets.Unresolved = unresolved
	writeAnalyticsJSON(w, http.StatusOK, out)
}
