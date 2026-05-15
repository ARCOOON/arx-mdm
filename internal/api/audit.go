package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditDeps wires GET /v1/audit (admin-only).
type AuditDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type auditListItem struct {
	ID              uuid.UUID       `json:"id"`
	Timestamp       time.Time       `json:"timestamp"`
	UserID          *uuid.UUID      `json:"user_id,omitempty"`
	Username        *string         `json:"username,omitempty"`
	Action          string          `json:"action"`
	TargetAssetID   *uuid.UUID      `json:"target_asset_id,omitempty"`
	TargetHumanID   *string         `json:"target_human_id,omitempty"`
	Details         json.RawMessage `json:"details,omitempty"`
}

type auditListResponse struct {
	Items []auditListItem `json:"items"`
	Total int64           `json:"total"`
}

// RegisterAuditRoutes registers GET /v1/audit with pagination and filters.
func RegisterAuditRoutes(mux *http.ServeMux, d AuditDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: audit routes require Pool, Logger, and Auth.JWT")
	}
	h := &auditHandler{deps: d}
	mux.HandleFunc("GET /v1/audit", h.handleList)
}

type auditHandler struct {
	deps AuditDeps
}

func (h *auditHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}

	limit := int64(50)
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := parseAuditLimitOffset(v, 200); err == nil {
			limit = n
		}
	}
	offset := int64(0)
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := parseAuditLimitOffset(v, 1<<30); err == nil {
			offset = n
		}
	}

	var filterUserID *uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("user_id")); v != "" {
		if uid, err := uuid.Parse(v); err == nil {
			filterUserID = &uid
		}
	}

	actionSub := strings.TrimSpace(r.URL.Query().Get("action"))

	var fromTS, toTS *time.Time
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.UTC); err == nil {
			fromTS = &t
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.UTC); err == nil {
			end := t.Add(24 * time.Hour)
			toTS = &end
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	countArgs := []any{filterUserID, actionSub, fromTS, toTS}
	var total int64
	err := h.deps.Pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM audit_logs a
WHERE ($1::uuid IS NULL OR a.user_id = $1)
  AND ($2::text = '' OR a.action ILIKE '%' || $2 || '%')
  AND ($3::timestamptz IS NULL OR a.logged_at >= $3)
  AND ($4::timestamptz IS NULL OR a.logged_at < $4)
`, countArgs...).Scan(&total)
	if err != nil {
		h.deps.Logger.Error("audit list count", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}

	qArgs := []any{filterUserID, actionSub, fromTS, toTS, limit, offset}
	rows, err := h.deps.Pool.Query(ctx, `
SELECT a.id, a.logged_at, a.user_id, u.username, a.action, a.target_asset_id, ast.human_id, a.details_json
FROM audit_logs a
LEFT JOIN users u ON u.id = a.user_id
LEFT JOIN assets ast ON ast.id = a.target_asset_id
WHERE ($1::uuid IS NULL OR a.user_id = $1)
  AND ($2::text = '' OR a.action ILIKE '%' || $2 || '%')
  AND ($3::timestamptz IS NULL OR a.logged_at >= $3)
  AND ($4::timestamptz IS NULL OR a.logged_at < $4)
ORDER BY a.logged_at DESC
LIMIT $5 OFFSET $6
`, qArgs...)
	if err != nil {
		h.deps.Logger.Error("audit list query", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	defer rows.Close()

	var items []auditListItem
	for rows.Next() {
		var row models.AuditLog
		var username *string
		var humanID *string
		if err := rows.Scan(&row.ID, &row.LoggedAt, &row.UserID, &username, &row.Action, &row.TargetAssetID, &humanID, &row.DetailsJSON); err != nil {
			h.deps.Logger.Error("audit list scan", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
			return
		}
		var details json.RawMessage
		if len(row.DetailsJSON) > 0 {
			details = json.RawMessage(append([]byte(nil), row.DetailsJSON...))
		}
		items = append(items, auditListItem{
			ID:            row.ID,
			Timestamp:     row.LoggedAt,
			UserID:        row.UserID,
			Username:      username,
			Action:        row.Action,
			TargetAssetID: row.TargetAssetID,
			TargetHumanID: humanID,
			Details:       details,
		})
	}
	if err := rows.Err(); err != nil {
		h.deps.Logger.Error("audit list rows", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	if items == nil {
		items = []auditListItem{}
	}
	writeTicketsJSON(w, http.StatusOK, auditListResponse{Items: items, Total: total})
}

func parseAuditLimitOffset(s string, max int64) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("invalid")
	}
	if n > max {
		return max, nil
	}
	return n, nil
}
