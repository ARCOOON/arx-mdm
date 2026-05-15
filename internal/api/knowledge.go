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

const maxDocumentJSONBody = 512 << 10

// KnowledgeDeps wires knowledge base document CRUD.
type KnowledgeDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
	Auth   DashboardAuth
}

type documentWire struct {
	ID              uuid.UUID  `json:"id"`
	Title           string     `json:"title"`
	ContentMarkdown string     `json:"content_markdown"`
	UploadedBy      *uuid.UUID `json:"uploaded_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type createDocumentRequest struct {
	Title           string `json:"title"`
	ContentMarkdown string `json:"content_markdown"`
}

type patchDocumentRequest struct {
	Title           *string `json:"title"`
	ContentMarkdown *string `json:"content_markdown"`
}

// RegisterKnowledgeRoutes registers /v1/documents CRUD.
func RegisterKnowledgeRoutes(mux *http.ServeMux, d KnowledgeDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: knowledge routes require Pool, Logger, and Auth.JWT")
	}
	h := &knowledgeHandler{deps: d}
	mux.HandleFunc("GET /v1/documents", h.handleList)
	mux.HandleFunc("POST /v1/documents", h.handleCreate)
	mux.HandleFunc("GET /v1/documents/{id}", h.handleGet)
	mux.HandleFunc("PATCH /v1/documents/{id}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/documents/{id}", h.handleDelete)
}

type knowledgeHandler struct {
	deps KnowledgeDeps
}

func parseDocUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, errors.New("missing id")
	}
	return uuid.Parse(raw)
}

func (h *knowledgeHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, title, COALESCE(content_markdown, ''), uploaded_by, created_at
FROM documents
ORDER BY created_at DESC
LIMIT 2000
`)
	if err != nil {
		h.deps.Logger.Error("list documents", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}
	defer rows.Close()
	var out []documentWire
	for rows.Next() {
		var d documentWire
		if err := rows.Scan(&d.ID, &d.Title, &d.ContentMarkdown, &d.UploadedBy, &d.CreatedAt); err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to list documents")
			return
		}
		out = append(out, d)
	}
	if out == nil {
		out = []documentWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *knowledgeHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDocumentJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createDocumentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeTicketsError(w, http.StatusBadRequest, "title is required")
		return
	}
	if utf8.RuneCountInString(req.ContentMarkdown) > 512_000 {
		writeTicketsError(w, http.StatusBadRequest, "content_markdown too large")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO documents (title, content_markdown, uploaded_by)
VALUES ($1, $2, $3)
RETURNING id
`, req.Title, req.ContentMarkdown, p.UserID).Scan(&id)
	if err != nil {
		h.deps.Logger.Error("insert document", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create document")
		return
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *knowledgeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleViewer); !ok {
		return
	}
	id, err := parseDocUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var d models.Document
	err = h.deps.Pool.QueryRow(ctx, `
SELECT id, title, COALESCE(content_markdown, ''), uploaded_by, created_at
FROM documents WHERE id = $1
`, id).Scan(&d.ID, &d.Title, &d.ContentMarkdown, &d.UploadedBy, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsError(w, http.StatusNotFound, "document not found")
		return
	}
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to load document")
		return
	}
	writeTicketsJSON(w, http.StatusOK, documentWire{
		ID:              d.ID,
		Title:           d.Title,
		ContentMarkdown: d.ContentMarkdown,
		UploadedBy:      d.UploadedBy,
		CreatedAt:       d.CreatedAt,
	})
}

func (h *knowledgeHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	id, err := parseDocUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxDocumentJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchDocumentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == nil && req.ContentMarkdown == nil {
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
	if req.ContentMarkdown != nil && utf8.RuneCountInString(*req.ContentMarkdown) > 512_000 {
		writeTicketsError(w, http.StatusBadRequest, "content_markdown too large")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var setParts []string
	args := make([]any, 0, 6)
	argPos := 1
	if req.Title != nil {
		setParts = append(setParts, fmt.Sprintf("title = $%d", argPos))
		args = append(args, *req.Title)
		argPos++
	}
	if req.ContentMarkdown != nil {
		setParts = append(setParts, fmt.Sprintf("content_markdown = $%d", argPos))
		args = append(args, *req.ContentMarkdown)
		argPos++
	}
	if len(setParts) == 0 {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	args = append(args, id)
	q := fmt.Sprintf("UPDATE documents SET %s WHERE id = $%d", strings.Join(setParts, ", "), argPos)
	tag, err := h.deps.Pool.Exec(ctx, q, args...)
	if err != nil {
		h.deps.Logger.Error("patch document", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to update document")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "document not found")
		return
	}
	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *knowledgeHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	id, err := parseDocUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM documents WHERE id = $1`, id)
	if err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete document")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "document not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
