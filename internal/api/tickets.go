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
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxTicketJSONBody = 512 << 10

// TicketsDeps wires the ticket REST API.
type TicketsDeps struct {
	Pool             *pgxpool.Pool
	Logger           *slog.Logger
	Auth             DashboardAuth
	OnTicketsMutated func() // optional: refresh dashboard ticket_snapshot subscribers
	// OnINCTicketCreated is optional; invoked after a new INC-* ticket is persisted.
	OnINCTicketCreated func(ctx context.Context, ticketRef, title string, linkedAssetID *uuid.UUID)
}

type ticketsJSONError struct {
	Error string `json:"error"`
}

func writeTicketsJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTicketsError(w http.ResponseWriter, status int, msg string) {
	writeTicketsJSON(w, status, ticketsJSONError{Error: msg})
}

var (
	allowedTicketKinds    = map[string]string{"INC": "INC", "REQ": "REQ", "CHG": "CHG", "PRJ": "PRJ"}
	allowedTicketPriority = map[string]struct{}{"critical": {}, "high": {}, "medium": {}, "low": {}}
	allowedTicketStatus   = map[string]struct{}{
		"open": {}, "in_progress": {}, "resolved": {}, "closed": {},
	}
)

func ticketSequenceName(kind string) (string, error) {
	k := allowedTicketKinds[strings.ToUpper(strings.TrimSpace(kind))]
	if k == "" {
		return "", fmt.Errorf("invalid ticket kind")
	}
	return "ticket_seq_" + strings.ToLower(k), nil
}

// TicketListRow is returned by GET /v1/tickets.
type TicketListRow struct {
	ID                 uuid.UUID  `json:"id"`
	TicketRef          string     `json:"ticket_ref"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	Status             string     `json:"status"`
	Priority           string     `json:"priority"`
	LinkedArxID        *string    `json:"linked_arx_id,omitempty"`
	DeviceID           *uuid.UUID `json:"device_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CreatedByUserID    *uuid.UUID `json:"created_by,omitempty"`
	CreatedByUsername  *string    `json:"created_by_username,omitempty"`
	AssignedToUserID   *uuid.UUID `json:"assigned_to,omitempty"`
	AssignedToUsername *string    `json:"assigned_to_username,omitempty"`
}

// TicketDetailResponse is returned by GET /v1/tickets/{id}.
type TicketDetailResponse struct {
	Ticket      TicketListRow       `json:"ticket"`
	Resolutions []models.Resolution `json:"resolutions"`
}

type createTicketRequest struct {
	Kind               string `json:"kind"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	Status             string `json:"status"`
	Priority           string `json:"priority"`
	LinkedAssetHumanID string `json:"linked_asset_human_id"`
	AssignedToUserID   string `json:"assigned_to_user_id"`
}

type patchTicketRequest struct {
	Title              *string `json:"title"`
	Description        *string `json:"description"`
	Status             *string `json:"status"`
	Priority           *string `json:"priority"`
	LinkedAssetHumanID *string `json:"linked_asset_human_id"`
	AssignedToUserID   *string `json:"assigned_to_user_id"`
	ClearAssignedTo    *bool   `json:"clear_assigned_to"`
}

type createResolutionRequest struct {
	Summary  string `json:"summary"`
	Markdown string `json:"markdown"`
}

func normalizePriority(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return "medium"
	}
	return p
}

func normalizeStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "open"
	}
	return s
}

func validatePriority(p string) bool {
	_, ok := allowedTicketPriority[p]
	return ok
}

func validateStatus(s string) bool {
	_, ok := allowedTicketStatus[s]
	return ok
}

func pointerStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// NewTicketsHandler registers dashboard-authenticated ticket CRUD on the given mux.
func NewTicketsHandler(mux *http.ServeMux, d TicketsDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: tickets handler requires Pool, Logger, and Auth.JWT")
	}
	h := &ticketsHandler{deps: d}
	mux.HandleFunc("GET /v1/tickets", h.handleList)
	mux.HandleFunc("POST /v1/tickets", h.handleCreate)
	mux.HandleFunc("GET /v1/tickets/{id}", h.handleGet)
	mux.HandleFunc("PATCH /v1/tickets/{id}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/tickets/{id}", h.handleDelete)
	mux.HandleFunc("POST /v1/tickets/{id}/resolutions", h.handlePostResolution)
}

type ticketsHandler struct {
	deps TicketsDeps
}

func (h *ticketsHandler) authorizeViewer(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer)
	return ok
}

func (h *ticketsHandler) authorizeOperator(w http.ResponseWriter, r *http.Request) bool {
	_, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	return ok
}

func (h *ticketsHandler) notifyMutated() {
	if h.deps.OnTicketsMutated == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			h.deps.Logger.Error("OnTicketsMutated panicked", "recover", rec)
		}
	}()
	h.deps.OnTicketsMutated()
}

func (h *ticketsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var deviceArg any
	filterDevice := strings.TrimSpace(r.URL.Query().Get("device_id"))
	if filterDevice != "" {
		id, err := uuid.Parse(filterDevice)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid device_id query parameter")
			return
		}
		deviceArg = id
	}

	rows, err := h.deps.Pool.Query(ctx, `
SELECT t.id, t.ticket_ref, t.title, t.description, t.status, t.priority, t.device_id, t.created_at, t.updated_at, a.human_id,
       t.created_by, cb.username,
       t.assigned_to, ab.username
FROM tickets t
LEFT JOIN assets a ON a.id = t.device_id
LEFT JOIN users cb ON cb.id = t.created_by
LEFT JOIN users ab ON ab.id = t.assigned_to
WHERE ($1::uuid IS NULL OR t.device_id = $1)
ORDER BY t.created_at DESC
LIMIT 1000
`, deviceArg)
	if err != nil {
		h.deps.Logger.Error("list tickets", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to list tickets")
		return
	}
	defer rows.Close()

	var out []TicketListRow
	for rows.Next() {
		var row TicketListRow
		var humanID *string
		var createdBy *uuid.UUID
		var createdByName *string
		var assignedTo *uuid.UUID
		var assignedToName *string
		if err := rows.Scan(
			&row.ID, &row.TicketRef, &row.Title, &row.Description, &row.Status, &row.Priority,
			&row.DeviceID,
			&row.CreatedAt, &row.UpdatedAt, &humanID,
			&createdBy, &createdByName,
			&assignedTo, &assignedToName,
		); err != nil {
			h.deps.Logger.Error("scan ticket row", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTicketsError(w, http.StatusInternalServerError, "failed to list tickets")
			return
		}
		if humanID != nil && *humanID != "" {
			row.LinkedArxID = humanID
		}
		if createdBy != nil {
			row.CreatedByUserID = createdBy
			if createdByName != nil && *createdByName != "" {
				row.CreatedByUsername = createdByName
			}
		}
		if assignedTo != nil {
			row.AssignedToUserID = assignedTo
			if assignedToName != nil && *assignedToName != "" {
				row.AssignedToUsername = assignedToName
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		h.deps.Logger.Error("ticket rows", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to list tickets")
		return
	}
	if out == nil {
		out = []TicketListRow{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *ticketsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTicketJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createTicketRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeTicketsError(w, http.StatusBadRequest, "title is required")
		return
	}
	if utf8.RuneCountInString(req.Description) > 64_000 {
		writeTicketsError(w, http.StatusBadRequest, "description too large")
		return
	}
	kind, ok := allowedTicketKinds[strings.ToUpper(strings.TrimSpace(req.Kind))]
	if !ok {
		writeTicketsError(w, http.StatusBadRequest, "kind must be one of INC, REQ, CHG, PRJ")
		return
	}
	st := normalizeStatus(req.Status)
	if !validateStatus(st) {
		writeTicketsError(w, http.StatusBadRequest, "invalid status")
		return
	}
	pr := normalizePriority(req.Priority)
	if !validatePriority(pr) {
		writeTicketsError(w, http.StatusBadRequest, "invalid priority")
		return
	}
	seqName, err := ticketSequenceName(kind)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tx, err := h.deps.Pool.Begin(ctx)
	if err != nil {
		h.deps.Logger.Error("ticket tx begin", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var seq int64
	// seqName is whitelisted (INC|REQ|CHG|PRJ → ticket_seq_*).
	qNext := fmt.Sprintf(`SELECT nextval('%s')`, seqName)
	if err := tx.QueryRow(ctx, qNext).Scan(&seq); err != nil {
		h.deps.Logger.Error("ticket sequence", "err", err, "seq", seqName, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to allocate ticket reference")
		return
	}
	ticketRef := fmt.Sprintf("%s-%05d", kind, seq)

	var deviceID *uuid.UUID
	hid := strings.TrimSpace(req.LinkedAssetHumanID)
	if hid != "" {
		var aid uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, hid).Scan(&aid)
		if errors.Is(err, pgx.ErrNoRows) {
			writeTicketsError(w, http.StatusBadRequest, "linked_asset_human_id not found")
			return
		}
		if err != nil {
			h.deps.Logger.Error("resolve asset", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
			return
		}
		deviceID = &aid
	}

	var assignedTo *uuid.UUID
	au := strings.TrimSpace(req.AssignedToUserID)
	if au != "" {
		assignID, err := uuid.Parse(au)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, "invalid assigned_to_user_id")
			return
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, assignID).Scan(&exists); err != nil {
			h.deps.Logger.Error("assignee lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			writeTicketsError(w, http.StatusInternalServerError, "failed to validate assignee")
			return
		}
		if !exists {
			writeTicketsError(w, http.StatusBadRequest, "assigned_to_user_id not found")
			return
		}
		assignedTo = &assignID
	}

	var newID uuid.UUID
	err = tx.QueryRow(ctx, `
INSERT INTO tickets (ticket_ref, title, description, status, priority, device_id, created_by, assigned_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id
`, ticketRef, req.Title, req.Description, st, pr, deviceID, p.UserID, assignedTo).Scan(&newID)
	if err != nil {
		h.deps.Logger.Error("insert ticket", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		h.deps.Logger.Error("ticket commit", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}

	h.notifyMutated()
	if kind == "INC" && h.deps.OnINCTicketCreated != nil {
		evCtx, evCancel := context.WithTimeout(context.Background(), 5*time.Second)
		go func() {
			defer evCancel()
			defer func() {
				if rec := recover(); rec != nil {
					h.deps.Logger.Error("OnINCTicketCreated panicked", "recover", rec)
				}
			}()
			h.deps.OnINCTicketCreated(evCtx, ticketRef, req.Title, deviceID)
		}()
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]any{
		"id":         newID,
		"ticket_ref": ticketRef,
	})
}

func parseTicketUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
	}
	return uuid.Parse(raw)
}

func (h *ticketsHandler) loadTicketDetail(ctx context.Context, id uuid.UUID) (TicketDetailResponse, error) {
	var out TicketDetailResponse
	row := h.deps.Pool.QueryRow(ctx, `
SELECT t.id, t.ticket_ref, t.title, t.description, t.status, t.priority, t.device_id, t.created_at, t.updated_at, a.human_id,
       t.created_by, cb.username,
       t.assigned_to, ab.username
FROM tickets t
LEFT JOIN assets a ON a.id = t.device_id
LEFT JOIN users cb ON cb.id = t.created_by
LEFT JOIN users ab ON ab.id = t.assigned_to
WHERE t.id = $1
`, id)
	var humanID *string
	var createdBy *uuid.UUID
	var createdByName *string
	var assignedTo *uuid.UUID
	var assignedToName *string
	err := row.Scan(
		&out.Ticket.ID, &out.Ticket.TicketRef, &out.Ticket.Title, &out.Ticket.Description, &out.Ticket.Status, &out.Ticket.Priority,
		&out.Ticket.DeviceID, &out.Ticket.CreatedAt, &out.Ticket.UpdatedAt, &humanID,
		&createdBy, &createdByName,
		&assignedTo, &assignedToName,
	)
	if err != nil {
		return out, err
	}
	if humanID != nil && *humanID != "" {
		out.Ticket.LinkedArxID = humanID
	}
	if createdBy != nil {
		out.Ticket.CreatedByUserID = createdBy
		if createdByName != nil && *createdByName != "" {
			out.Ticket.CreatedByUsername = createdByName
		}
	}
	if assignedTo != nil {
		out.Ticket.AssignedToUserID = assignedTo
		if assignedToName != nil && *assignedToName != "" {
			out.Ticket.AssignedToUsername = assignedToName
		}
	}

	rrows, err := h.deps.Pool.Query(ctx, `
SELECT id, ticket_id, summary, COALESCE(markdown, ''), details, resolved_at, created_at
FROM resolutions
WHERE ticket_id = $1
ORDER BY created_at ASC, id ASC
`, id)
	if err != nil {
		return out, err
	}
	defer rrows.Close()

	for rrows.Next() {
		var res models.Resolution
		if err := rrows.Scan(&res.ID, &res.TicketID, &res.Summary, &res.Markdown, &res.Details, &res.ResolvedAt, &res.CreatedAt); err != nil {
			return out, err
		}
		res.Details = nil
		out.Resolutions = append(out.Resolutions, res)
	}
	return out, rrows.Err()
}

func (h *ticketsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeViewer(w, r) {
		return
	}
	id, err := parseTicketUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid ticket id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	detail, err := h.loadTicketDetail(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err != nil {
		h.deps.Logger.Error("get ticket", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to load ticket")
		return
	}
	writeTicketsJSON(w, http.StatusOK, detail)
}

func (h *ticketsHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseTicketUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid ticket id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTicketJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchTicketRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == nil && req.Description == nil && req.Status == nil && req.Priority == nil &&
		req.LinkedAssetHumanID == nil && req.AssignedToUserID == nil && req.ClearAssignedTo == nil {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if req.Title != nil {
		*req.Title = strings.TrimSpace(*req.Title)
		if *req.Title == "" {
			writeTicketsError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
	}
	if req.Description != nil && utf8.RuneCountInString(*req.Description) > 64_000 {
		writeTicketsError(w, http.StatusBadRequest, "description too large")
		return
	}
	if req.ClearAssignedTo != nil && *req.ClearAssignedTo && strings.TrimSpace(pointerStr(req.AssignedToUserID)) != "" {
		writeTicketsError(w, http.StatusBadRequest, "clear_assigned_to conflicts with assigned_to_user_id")
		return
	}
	if req.Status != nil {
		s := normalizeStatus(*req.Status)
		if !validateStatus(s) {
			writeTicketsError(w, http.StatusBadRequest, "invalid status")
			return
		}
		req.Status = &s
	}
	if req.Priority != nil {
		p := normalizePriority(*req.Priority)
		if !validatePriority(p) {
			writeTicketsError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		req.Priority = &p
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tx, err := h.deps.Pool.Begin(ctx)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to update ticket")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var setParts []string
	args := make([]any, 0, 8)
	argPos := 1

	if req.Title != nil {
		setParts = append(setParts, fmt.Sprintf("title = $%d", argPos))
		args = append(args, *req.Title)
		argPos++
	}
	if req.Description != nil {
		setParts = append(setParts, fmt.Sprintf("description = $%d", argPos))
		args = append(args, *req.Description)
		argPos++
	}
	if req.Status != nil {
		setParts = append(setParts, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *req.Status)
		argPos++
	}
	if req.Priority != nil {
		setParts = append(setParts, fmt.Sprintf("priority = $%d", argPos))
		args = append(args, *req.Priority)
		argPos++
	}
	if req.LinkedAssetHumanID != nil {
		hv := strings.TrimSpace(*req.LinkedAssetHumanID)
		if hv == "" {
			setParts = append(setParts, "device_id = NULL")
		} else {
			var did uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1`, hv).Scan(&did)
			if errors.Is(err, pgx.ErrNoRows) {
				writeTicketsError(w, http.StatusBadRequest, "linked_asset_human_id not found")
				return
			}
			if err != nil {
				h.deps.Logger.Error("patch resolve asset", "err", err, "request_id", r.Header.Get("X-Request-Id"))
				writeTicketsError(w, http.StatusInternalServerError, "failed to resolve asset")
				return
			}
			setParts = append(setParts, fmt.Sprintf("device_id = $%d", argPos))
			args = append(args, did)
			argPos++
		}
	}
	if req.ClearAssignedTo != nil && *req.ClearAssignedTo {
		setParts = append(setParts, "assigned_to = NULL")
	} else if req.AssignedToUserID != nil {
		atu := strings.TrimSpace(*req.AssignedToUserID)
		if atu == "" {
			setParts = append(setParts, "assigned_to = NULL")
		} else {
			assignID, err := uuid.Parse(atu)
			if err != nil {
				writeTicketsError(w, http.StatusBadRequest, "invalid assigned_to_user_id")
				return
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, assignID).Scan(&exists); err != nil {
				h.deps.Logger.Error("patch assignee lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
				writeTicketsError(w, http.StatusInternalServerError, "failed to validate assignee")
				return
			}
			if !exists {
				writeTicketsError(w, http.StatusBadRequest, "assigned_to_user_id not found")
				return
			}
			setParts = append(setParts, fmt.Sprintf("assigned_to = $%d", argPos))
			args = append(args, assignID)
			argPos++
		}
	}

	if len(setParts) == 0 {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	setParts = append(setParts, "updated_at = now()")
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE tickets SET %s WHERE id = $%d`, strings.Join(setParts, ", "), argPos)

	tag, err := tx.Exec(ctx, q, args...)
	if err != nil {
		h.deps.Logger.Error("patch ticket", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to update ticket")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to update ticket")
		return
	}

	h.notifyMutated()
	detail, err := h.loadTicketDetail(ctx, id)
	if err != nil {
		h.deps.Logger.Error("reload ticket after patch", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	writeTicketsJSON(w, http.StatusOK, detail)
}

func (h *ticketsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseTicketUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid ticket id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM tickets WHERE id = $1`, id)
	if err != nil {
		h.deps.Logger.Error("delete ticket", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete ticket")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "ticket not found")
		return
	}
	h.notifyMutated()
	w.WriteHeader(http.StatusNoContent)
}

func (h *ticketsHandler) handlePostResolution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeOperator(w, r) {
		return
	}
	id, err := parseTicketUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid ticket id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTicketJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createResolutionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if req.Summary == "" {
		writeTicketsError(w, http.StatusBadRequest, "summary is required")
		return
	}
	if utf8.RuneCountInString(req.Markdown) > 256_000 {
		writeTicketsError(w, http.StatusBadRequest, "markdown too large")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var exists bool
	if err := h.deps.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tickets WHERE id = $1)`, id).Scan(&exists); err != nil {
		h.deps.Logger.Error("resolution ticket lookup", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create resolution")
		return
	}
	if !exists {
		writeTicketsError(w, http.StatusNotFound, "ticket not found")
		return
	}

	var resID uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO resolutions (ticket_id, summary, markdown, details)
VALUES ($1, $2, $3, '{}'::jsonb)
RETURNING id
`, id, req.Summary, req.Markdown).Scan(&resID)
	if err != nil {
		h.deps.Logger.Error("insert resolution", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		writeTicketsError(w, http.StatusInternalServerError, "failed to create resolution")
		return
	}

	h.notifyMutated()
	writeTicketsJSON(w, http.StatusCreated, map[string]any{"id": resID})
}
