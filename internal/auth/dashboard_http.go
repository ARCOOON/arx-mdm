package auth

import (
	"net/http"
	"strings"
)

// DashboardOriginOK reports whether a browser Origin header is absent or on the allowlist.
func DashboardOriginOK(r *http.Request, allowedOrigins []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	for _, o := range allowedOrigins {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

// DashboardBearerToken reads a JWT from ?token= or Authorization: Bearer.
func DashboardBearerToken(r *http.Request) string {
	got := strings.TrimSpace(r.URL.Query().Get("token"))
	if got != "" {
		return got
	}
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
