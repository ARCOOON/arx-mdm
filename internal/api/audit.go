package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"
	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditDeps wires audit log listing (admin-only).
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
	ResourceType    string          `json:"resource_type,omitempty"`
	ResourceID      *uuid.UUID      `json:"resource_id,omitempty"`
	TargetAssetID   *uuid.UUID      `json:"target_asset_id,omitempty"`
	TargetHumanID   *string         `json:"target_human_id,omitempty"`
	IPAddress       *string         `json:"ip_address,omitempty"`
	Details         json.RawMessage `json:"details,omitempty"`
}

type auditListResponse struct {
	Items []auditListItem `json:"items"`
	Total int64           `json:"total"`
}

// RegisterAuditRoutes registers GET /v1/audit and GET /v1/audit-logs (admin-only, same handler).
func RegisterAuditRoutes(mux *http.ServeMux, d AuditDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: audit routes require Pool, Logger, and Auth.JWT")
	}
	h := &auditHandler{deps: d}
	mux.HandleFunc("GET /v1/audit", h.handleList)
	mux.HandleFunc("GET /v1/audit-logs", h.handleList)
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
		if n, err := database.ParseAuditLimit(v, 200); err == nil {
			limit = n
		}
	}
	offset := int64(0)
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := database.ParseAuditLimit(v, 1<<30); err == nil {
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
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	sortDesc := true
	if v := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("sort"))); v == "asc" || v == "created_at_asc" {
		sortDesc = false
	}
	if v := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("order"))); v == "asc" {
		sortDesc = false
	}

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

	f := database.AuditLogFilter{
		UserID:       filterUserID,
		ActionSubstr: actionSub,
		ResourceType: resourceType,
		SortDesc:     sortDesc,
		FromTS:       fromTS,
		ToTS:         toTS,
		Limit:        limit,
		Offset:       offset,
	}
	total, err := database.CountAuditLogs(ctx, h.deps.Pool, f)
	if err != nil {
		h.deps.Logger.Error("audit list count", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	rows, err := database.ListAuditLogs(ctx, h.deps.Pool, f)
	if err != nil {
		h.deps.Logger.Error("audit list query", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}

	userIDs := make([]uuid.UUID, 0, len(rows))
	seenU := map[uuid.UUID]struct{}{}
	assetIDs := make([]uuid.UUID, 0, len(rows))
	seenA := map[uuid.UUID]struct{}{}
	for _, row := range rows {
		if row.UserID != nil {
			if _, ok := seenU[*row.UserID]; !ok {
				seenU[*row.UserID] = struct{}{}
				userIDs = append(userIDs, *row.UserID)
			}
		}
		if row.TargetAssetID != nil {
			if _, ok := seenA[*row.TargetAssetID]; !ok {
				seenA[*row.TargetAssetID] = struct{}{}
				assetIDs = append(assetIDs, *row.TargetAssetID)
			}
		}
	}
	unames, err := database.AuditLogUsernames(ctx, h.deps.Pool, userIDs)
	if err != nil {
		h.deps.Logger.Error("audit usernames", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	humans, err := database.AuditLogHumanIDs(ctx, h.deps.Pool, assetIDs)
	if err != nil {
		h.deps.Logger.Error("audit human ids", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}

	items := make([]auditListItem, 0, len(rows))
	for _, row := range rows {
		var username *string
		if row.UserID != nil {
			if n, ok := unames[*row.UserID]; ok && n != "" {
				u := n
				username = &u
			}
		}
		var humanID *string
		if row.TargetAssetID != nil {
			if hID, ok := humans[*row.TargetAssetID]; ok && hID != "" {
				hi := hID
				humanID = &hi
			}
		}
		var details json.RawMessage
		if len(row.DetailsJSON) > 0 {
			details = json.RawMessage(append([]byte(nil), row.DetailsJSON...))
		}
		items = append(items, auditListItem{
			ID:            row.ID,
			Timestamp:     row.CreatedAt,
			UserID:        row.UserID,
			Username:      username,
			Action:        row.Action,
			ResourceType:  row.ResourceType,
			ResourceID:    row.ResourceID,
			TargetAssetID: row.TargetAssetID,
			TargetHumanID: humanID,
			IPAddress:     row.IPAddress,
			Details:       details,
		})
	}
	if items == nil {
		items = []auditListItem{}
	}
	writeTicketsJSON(w, http.StatusOK, auditListResponse{Items: items, Total: total})
}
