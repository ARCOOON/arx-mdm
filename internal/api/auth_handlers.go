package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAuthJSONBody = 16 << 10

// AuthDeps wires login and session introspection.
type AuthDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	JWT     *auth.JWTService
	Origins []string
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      userWire  `json:"user"`
}

type userWire struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
}

// RegisterAuthRoutes registers POST /v1/auth/login and GET /v1/auth/me.
func RegisterAuthRoutes(mux *http.ServeMux, d AuthDeps) {
	if d.Pool == nil || d.Logger == nil || d.JWT == nil {
		panic("api: auth routes require Pool, Logger, and JWT")
	}
	h := &authHandler{deps: d}
	mux.HandleFunc("POST /v1/auth/login", h.handleLogin)
	mux.HandleFunc("GET /v1/auth/me", h.handleMe)
}

type authHandler struct {
	deps AuthDeps
}

func (h *authHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !DashboardOriginOK(r, h.deps.Origins) {
		writeTicketsError(w, http.StatusForbidden, "forbidden")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAuthJSONBody))
	if cerr := r.Body.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		writeTicketsError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	var req loginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeTicketsError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeTicketsError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	id, hash, role, err := auth.GetUserByUsername(ctx, h.deps.Pool, req.Username)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			h.deps.Logger.Debug("login lookup failed", "err", err)
		}
		writeTicketsError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		writeTicketsError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	roleNorm, ok := auth.ParseRole(role)
	if !ok {
		writeTicketsError(w, http.StatusInternalServerError, "invalid user role")
		return
	}

	p := auth.Principal{UserID: id, Username: req.Username, Role: roleNorm}
	token, exp, err := h.deps.JWT.Issue(p)
	if err != nil {
		h.deps.Logger.Error("jwt issue failed", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeTicketsJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: exp,
		User: userWire{
			ID:       id,
			Username: req.Username,
			Role:     roleNorm,
		},
	})

	actx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := auth.InsertAuditLog(actx, h.deps.Pool, id, "auth.login", nil, map[string]any{"username": req.Username}); err != nil {
		h.deps.Logger.Debug("audit login log failed", "err", err)
	}
}

func (h *authHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeTicketsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	authz := DashboardAuth{JWT: h.deps.JWT, Origins: h.deps.Origins}
	p, ok := authz.RequirePrincipal(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var uw userWire
	err := h.deps.Pool.QueryRow(ctx, `
SELECT id, username, role FROM users WHERE id = $1
`, p.UserID).Scan(&uw.ID, &uw.Username, &uw.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		writeTicketsError(w, http.StatusUnauthorized, "user no longer exists")
		return
	}
	if err != nil {
		h.deps.Logger.Error("auth me", "err", err)
		writeTicketsError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if rnorm, ok := auth.ParseRole(uw.Role); ok {
		uw.Role = rnorm
	}
	writeTicketsJSON(w, http.StatusOK, uw)
}
