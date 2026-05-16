package auth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// DashboardRBACMiddleware enforces dashboard JWT presence and role rules for /v1 APIs.
// It attaches the principal to the request context when authentication succeeds.
// Public routes (agent telemetry, enrollment, login, websocket upgrade) are excluded.
func DashboardRBACMiddleware(jwtSvc *JWTService, allowedOrigins []string) func(http.Handler) http.Handler {
	if jwtSvc == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			method := r.Method

			if !strings.HasPrefix(path, "/v1/") {
				next.ServeHTTP(w, r)
				return
			}
			if method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if dashboardRBACPublic(method, path) {
				next.ServeHTTP(w, r)
				return
			}

			if !DashboardOriginOK(r, allowedOrigins) {
				writeRBACJSON(w, http.StatusForbidden, "forbidden")
				return
			}

			tok := DashboardBearerToken(r)
			if tok == "" {
				writeRBACJSON(w, http.StatusUnauthorized, "missing token")
				return
			}
			p, err := jwtSvc.Parse(tok)
			if err != nil || p.UserID == uuid.Nil {
				writeRBACJSON(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := WithPrincipal(r.Context(), p)
			r = r.WithContext(ctx)

			if p.Role == RoleViewer && !viewerSafeMethod(method) {
				writeRBACJSON(w, http.StatusForbidden, "read-only role cannot modify data")
				return
			}
			if p.Role == RoleOperator && operatorForbidden(method, path) {
				writeRBACJSON(w, http.StatusForbidden, "insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeRBACJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func dashboardRBACPublic(method, path string) bool {
	switch {
	case method == http.MethodPost && path == "/v1/auth/login":
		return true
	case method == http.MethodPost && path == "/v1/enroll":
		return true
	case method == http.MethodPost && path == "/v1/telemetry":
		return true
	case method == http.MethodGet && path == "/v1/ws":
		return true
	default:
		return false
	}
}

func viewerSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func operatorForbidden(method, path string) bool {
	p := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if p == "" {
		p = "/"
	}
	if strings.HasPrefix(p, "/v1/audit") {
		return true
	}
	switch {
	case p == "/v1/users" && method == http.MethodGet:
		return true
	case p == "/v1/users" && method == http.MethodPost:
		return true
	case strings.HasPrefix(p, "/v1/users/") && p != "/v1/users/directory":
		if method == http.MethodPatch || method == http.MethodDelete {
			return true
		}
	case strings.HasPrefix(p, "/v1/alerts/"):
		if p == "/v1/alerts/active" && method == http.MethodGet {
			return false
		}
		return true
	case method == http.MethodDelete && strings.HasPrefix(p, "/v1/packages/"):
		return true
	}
	return false
}
