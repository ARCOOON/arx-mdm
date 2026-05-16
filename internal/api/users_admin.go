package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxUserAdminJSON = 32 << 10

// UsersAdminDeps wires admin-only user management routes.
type UsersAdminDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Auth    DashboardAuth
}

type userAdminWire struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type patchUserRequest struct {
	Role     *string `json:"role"`
	Password *string `json:"password"`
}

// RegisterUsersAdminRoutes registers /v1/users (admin only).
func RegisterUsersAdminRoutes(mux *http.ServeMux, d UsersAdminDeps) {
	if d.Pool == nil || d.Logger == nil || d.Auth.JWT == nil {
		panic("api: users admin requires Pool, Logger, and Auth.JWT")
	}
	h := &usersAdminHandler{deps: d}
	mux.HandleFunc("GET /v1/users", h.handleList)
	mux.HandleFunc("GET /v1/users/directory", h.handleDirectory)
	mux.HandleFunc("POST /v1/users", h.handleCreate)
	mux.HandleFunc("PATCH /v1/users/{id}", h.handlePatch)
	mux.HandleFunc("DELETE /v1/users/{id}", h.handleDelete)
}

type usersAdminHandler struct {
	deps UsersAdminDeps
}

func parseUserUUID(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" {
		return uuid.Nil, fmt.Errorf("missing id")
	}
	return uuid.Parse(raw)
}

func (h *usersAdminHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.deps.Pool.Query(ctx, `
SELECT id, username, role, created_at FROM users ORDER BY username ASC
`)
	if err != nil {
		h.deps.Logger.Error("list users", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()
	var out []userAdminWire
	for rows.Next() {
		var u userAdminWire
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		out = append(out, u)
	}
	if out == nil {
		out = []userAdminWire{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

type userDirectoryRow struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
}

// handleDirectory returns id and username for every dashboard account (operators use this for assignments).
func (h *usersAdminHandler) handleDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleOperator); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rows, err := h.deps.Pool.Query(ctx, `SELECT id, username FROM users ORDER BY username ASC`)
	if err != nil {
		h.deps.Logger.Error("list user directory", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	defer rows.Close()

	var out []userDirectoryRow
	for rows.Next() {
		var u userDirectoryRow
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		writeTicketsError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	if out == nil {
		out = []userDirectoryRow{}
	}
	writeTicketsJSON(w, http.StatusOK, out)
}

func (h *usersAdminHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUserAdminJSON))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req createUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	role, ok := auth.ParseRole(req.Role)
	if !ok {
		writeTicketsError(w, http.StatusBadRequest, "invalid role")
		return
	}
	if req.Username == "" || strings.TrimSpace(req.Password) == "" {
		writeTicketsError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var id uuid.UUID
	err = h.deps.Pool.QueryRow(ctx, `
INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING id
`, req.Username, hash, role).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeTicketsError(w, http.StatusConflict, "username already exists")
			return
		}
		h.deps.Logger.Error("create user", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeTicketsJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (h *usersAdminHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin); !ok {
		return
	}
	id, err := parseUserUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUserAdminJSON))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req patchUserRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Role == nil && req.Password == nil {
		writeTicketsError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if req.Role != nil {
		newRole, ok := auth.ParseRole(*req.Role)
		if !ok {
			writeTicketsError(w, http.StatusBadRequest, "invalid role")
			return
		}
		last, err := auth.IsLastAdmin(ctx, h.deps.Pool, id)
		if err != nil {
			h.deps.Logger.Error("last admin check", "err", err)
			writeTicketsError(w, http.StatusInternalServerError, "failed to update user")
			return
		}
		if last && newRole != auth.RoleAdmin {
			writeTicketsError(w, http.StatusBadRequest, "cannot demote the last admin")
			return
		}
		tag, err := h.deps.Pool.Exec(ctx, `UPDATE users SET role = $1 WHERE id = $2`, newRole, id)
		if err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to update user")
			return
		}
		if tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if req.Password != nil {
		pw := strings.TrimSpace(*req.Password)
		if pw == "" {
			writeTicketsError(w, http.StatusBadRequest, "password cannot be empty")
			return
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			writeTicketsError(w, http.StatusBadRequest, err.Error())
			return
		}
		tag, err := h.deps.Pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id)
		if err != nil {
			writeTicketsError(w, http.StatusInternalServerError, "failed to update user")
			return
		}
		if tag.RowsAffected() == 0 {
			writeTicketsError(w, http.StatusNotFound, "user not found")
			return
		}
	}
	writeTicketsJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *usersAdminHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, ok := h.deps.Auth.RequireMinRole(w, r, auth.RoleAdmin)
	if !ok {
		return
	}
	id, err := parseUserUUID(r)
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if id == p.UserID {
		writeTicketsError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	last, err := auth.IsLastAdmin(ctx, h.deps.Pool, id)
	if err != nil {
		h.deps.Logger.Error("last admin check", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	if last {
		writeTicketsError(w, http.StatusBadRequest, "cannot delete the last admin")
		return
	}
	tag, err := h.deps.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		h.deps.Logger.Error("delete user", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	if tag.RowsAffected() == 0 {
		writeTicketsError(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
