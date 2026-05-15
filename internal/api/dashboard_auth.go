package api

import (
	"net/http"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/google/uuid"
)

// DashboardAuth carries JWT verification and browser Origin allowlisting for dashboard HTTP APIs.
type DashboardAuth struct {
	JWT     *auth.JWTService
	Origins []string
}

// DashboardOriginOK delegates to auth for a single implementation (shared with audit middleware).
func DashboardOriginOK(r *http.Request, allowedOrigins []string) bool {
	return auth.DashboardOriginOK(r, allowedOrigins)
}

// DashboardBearerToken delegates to auth for a single implementation (shared with audit middleware).
func DashboardBearerToken(r *http.Request) string {
	return auth.DashboardBearerToken(r)
}

// RequirePrincipal validates Origin, JWT, and returns the dashboard principal.
func (d DashboardAuth) RequirePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if d.JWT == nil {
		writeTicketsError(w, http.StatusInternalServerError, "authentication not configured")
		return auth.Principal{}, false
	}
	if !auth.DashboardOriginOK(r, d.Origins) {
		writeTicketsError(w, http.StatusForbidden, "forbidden")
		return auth.Principal{}, false
	}
	if p, ok := auth.PrincipalFromContext(r.Context()); ok && p.UserID != uuid.Nil {
		return p, true
	}
	tok := auth.DashboardBearerToken(r)
	if tok == "" {
		writeTicketsError(w, http.StatusUnauthorized, "missing token")
		return auth.Principal{}, false
	}
	p, err := d.JWT.Parse(tok)
	if err != nil {
		writeTicketsError(w, http.StatusUnauthorized, "invalid token")
		return auth.Principal{}, false
	}
	return p, true
}

// RequireMinRole is like RequirePrincipal but enforces a minimum RBAC role.
func (d DashboardAuth) RequireMinRole(w http.ResponseWriter, r *http.Request, minRole string) (auth.Principal, bool) {
	p, ok := d.RequirePrincipal(w, r)
	if !ok {
		return auth.Principal{}, false
	}
	if !auth.HasAtLeast(p.Role, minRole) {
		writeTicketsError(w, http.StatusForbidden, "forbidden")
		return auth.Principal{}, false
	}
	return p, true
}
