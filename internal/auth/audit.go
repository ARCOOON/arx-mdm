package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var assetPathUUID = regexp.MustCompile(`^/v1/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})(/|$)`)

var dedicatedRESTAuditSkip = regexp.MustCompile(
	`^(/v1/devices/(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/(commands|assign|unassign|lock|wipe)` +
		`|/v1/incidents/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}/commands)$`,
)

// AuditRecord is the payload for a single audit_logs insert.
type AuditRecord struct {
	UserID        uuid.UUID
	Action        string
	ResourceType  string
	ResourceID    *uuid.UUID
	TargetAssetID *uuid.UUID
	Details       map[string]any
	IPAddress     string
}

// InsertAuditRecord appends one audit row (structured fields + JSON details).
func InsertAuditRecord(ctx context.Context, pool *pgxpool.Pool, rec AuditRecord) error {
	if pool == nil || strings.TrimSpace(rec.Action) == "" {
		return nil
	}
	details := rec.Details
	if len(details) == 0 {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var uid *uuid.UUID
	if rec.UserID != uuid.Nil {
		u := rec.UserID
		uid = &u
	}
	rt := strings.TrimSpace(rec.ResourceType)
	var ip *string
	if s := strings.TrimSpace(rec.IPAddress); s != "" {
		ip = &s
	}
	row := models.AuditLog{
		UserID:        uid,
		Action:        strings.TrimSpace(rec.Action),
		ResourceType:  rt,
		ResourceID:    rec.ResourceID,
		TargetAssetID: rec.TargetAssetID,
		DetailsJSON:   b,
		IPAddress:     ip,
	}
	return database.InsertAuditLogRow(ctx, pool, row)
}

// InsertAuditLog is a convenience wrapper; device-scoped actions set resource_type device when target is set.
func InsertAuditLog(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, action string, targetAssetID *uuid.UUID, details map[string]any) error {
	rec := AuditRecord{
		UserID:        userID,
		Action:        action,
		TargetAssetID: targetAssetID,
		Details:       details,
	}
	if targetAssetID != nil {
		rec.ResourceType = "device"
		rec.ResourceID = targetAssetID
	}
	return InsertAuditRecord(ctx, pool, rec)
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
	if _, ok := skipHTTPAudit[key]; ok {
		return true
	}
	if method == http.MethodPost && dedicatedRESTAuditSkip.MatchString(path) {
		return true
	}
	return false
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

func parsePathResource(path string) (string, *uuid.UUID) {
	path = strings.TrimSuffix(path, "/")
	prefixes := []struct {
		prefix string
		typ    string
	}{
		{"/v1/devices/", "device"},
		{"/v1/assets/", "asset"},
		{"/v1/incidents/", "incident"},
		{"/v1/users/", "user"},
	}
	for _, pr := range prefixes {
		idx := strings.Index(path, pr.prefix)
		if idx < 0 {
			continue
		}
		rest := path[idx+len(pr.prefix):]
		if rest == "" {
			continue
		}
		seg := strings.Split(rest, "/")[0]
		id, err := uuid.Parse(seg)
		if err != nil {
			continue
		}
		return pr.typ, &id
	}
	return "", nil
}

// MutatingAuditMiddleware logs successful (2xx) authenticated dashboard REST mutations to audit_logs.
// It uses the dashboard principal from context (attached by DashboardRBACMiddleware) when present,
// otherwise attempts JWT parsing for compatibility.
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
			if p, ok := PrincipalFromContext(ctx); !ok || p.UserID == uuid.Nil {
				if DashboardOriginOK(r, allowedOrigins) {
					tok := DashboardBearerToken(r)
					if tok != "" {
						if p, err := jwtSvc.Parse(tok); err == nil && p.UserID != uuid.Nil {
							ctx = WithPrincipal(ctx, p)
							r = r.WithContext(ctx)
						}
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

			resType, resID := parsePathResource(path)
			target := parseOptionalAssetTarget(path)
			if target == nil && resType == "device" && resID != nil {
				target = resID
			}
			ip := ClientIP(r)
			details := map[string]any{
				"http_method": method,
				"path":        path,
				"http_status": lw.status,
			}
			if rid := strings.TrimSpace(r.Header.Get("X-Request-Id")); rid != "" {
				details["request_id"] = rid
			}

			actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rec := AuditRecord{
				UserID:        p.UserID,
				Action:        "rest_mutation",
				ResourceType:  resType,
				ResourceID:    resID,
				TargetAssetID: target,
				Details:       details,
				IPAddress:     ip,
			}
			if err := InsertAuditRecord(actx, pool, rec); err != nil && logger != nil {
				logger.Warn("audit log insert failed", "err", err, "action", rec.Action, "user_id", p.UserID.String())
			}
		})
	}
}
