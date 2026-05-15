package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var assetPathUUID = regexp.MustCompile(`^/v1/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})(/|$)`)

// InsertAuditLog appends one audit row. userID may be uuid.Nil to persist NULL (omit with nil pointer in SQL).
func InsertAuditLog(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, action string, targetAssetID *uuid.UUID, details map[string]any) error {
	if pool == nil || strings.TrimSpace(action) == "" {
		return nil
	}
	var det []byte
	if len(details) == 0 {
		det = []byte("{}")
	} else {
		var err error
		det, err = json.Marshal(details)
		if err != nil {
			return err
		}
	}
	var uid any
	if userID == uuid.Nil {
		uid = nil
	} else {
		uid = userID
	}
	_, err := pool.Exec(ctx, `
INSERT INTO audit_logs (user_id, action, target_asset_id, details_json)
VALUES ($1, $2, $3, $4::jsonb)
`, uid, strings.TrimSpace(action), targetAssetID, det)
	return err
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (a *auditResponseWriter) WriteHeader(code int) {
	if a.status == 0 {
		a.status = code
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *auditResponseWriter) Write(b []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	return a.ResponseWriter.Write(b)
}

// skipHTTPAudit lists mutating routes without dashboard JWT (middleware must not expect a principal).
var skipHTTPAudit = map[string]struct{}{
	"POST /v1/auth/login": {},
	"POST /v1/enroll":     {},
	"POST /v1/telemetry":  {},
}

func skipMutatingHTTPAudit(method, path string) bool {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	key := method + " " + path
	_, ok := skipHTTPAudit[key]
	return ok
}

func parseOptionalAssetTarget(path string) *uuid.UUID {
	m := assetPathUUID.FindStringSubmatch(path)
	if len(m) < 2 {
		return nil
	}
	id, err := uuid.Parse(m[1])
	if err != nil {
		return nil
	}
	return &id
}

// MutatingAuditMiddleware logs successful (2xx) authenticated dashboard REST mutations to audit_logs.
// It parses the JWT (same rules as dashboard APIs: Origin allowlist + Bearer / ?token=) and attaches the
// principal to request context for downstream handlers.
func MutatingAuditMiddleware(pool *pgxpool.Pool, logger *slog.Logger, jwtSvc *JWTService, allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pool == nil || jwtSvc == nil {
				next.ServeHTTP(w, r)
				return
			}
			method := r.Method
			switch method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			default:
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path
			if !strings.HasPrefix(path, "/v1/") {
				next.ServeHTTP(w, r)
				return
			}
			if skipMutatingHTTPAudit(method, path) {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			if DashboardOriginOK(r, allowedOrigins) {
				tok := DashboardBearerToken(r)
				if tok != "" {
					if p, err := jwtSvc.Parse(tok); err == nil && p.UserID != uuid.Nil {
						ctx = WithPrincipal(ctx, p)
						r = r.WithContext(ctx)
					}
				}
			}

			lw := &auditResponseWriter{ResponseWriter: w, status: 0}
			next.ServeHTTP(lw, r)

			if lw.status < 200 || lw.status > 299 {
				return
			}
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p.UserID == uuid.Nil {
				return
			}

			action := "REST " + method + " " + path
			target := parseOptionalAssetTarget(path)
			details := map[string]any{
				"http_status": lw.status,
			}
			if rid := strings.TrimSpace(r.Header.Get("X-Request-Id")); rid != "" {
				details["request_id"] = rid
			}

			actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := InsertAuditLog(actx, pool, p.UserID, action, target, details); err != nil && logger != nil {
				logger.Warn("audit log insert failed", "err", err, "action", action, "user_id", p.UserID.String())
			}
		})
	}
}
