package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"
	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	wsReadDeadline  = 90 * time.Second
	wsWriteDeadline = 15 * time.Second
)

// WSGatewayDeps configures the shared GET /v1/ws upgrade endpoint for agents and operator dashboards.
type WSGatewayDeps struct {
	C2Hub            *Hub
	DashboardHub     *DashboardHub
	Pool             *pgxpool.Pool
	Logger           *slog.Logger
	MTLSRequired     bool
	DashboardJWT     *auth.JWTService
	DashboardOrigins []string // allowed browser Origins for Sec-WebSocket-Protocol: arx-dashboard
	AgentTelemetry   AgentTelemetryDeps
}

func dashboardProtoRequested(r *http.Request) bool {
	raw := r.Header.Get("Sec-WebSocket-Protocol")
	for _, p := range strings.Split(raw, ",") {
		if strings.TrimSpace(p) == "arx-dashboard" {
			return true
		}
	}
	return false
}

func originAllowed(r *http.Request, allowed []string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	for _, o := range allowed {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

func dashboardJWTPrincipal(r *http.Request, jwtSvc *auth.JWTService, allowedOrigins []string) (auth.Principal, error) {
	if jwtSvc == nil {
		return auth.Principal{}, errors.New("jwt not configured")
	}
	if !originAllowed(r, allowedOrigins) {
		return auth.Principal{}, errors.New("origin not allowed")
	}
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if tok == "" {
		return auth.Principal{}, errors.New("missing token")
	}
	return jwtSvc.Parse(tok)
}

// NewWSGatewayHandler serves GET /v1/ws: agent C2 (mutual TLS, no browser Origin) or
// operator dashboard fan-out (Sec-WebSocket-Protocol: arx-dashboard, browser Origin allowlist).
func NewWSGatewayHandler(d WSGatewayDeps) http.HandlerFunc {
	if d.C2Hub == nil {
		panic("ws: C2Hub is nil")
	}
	if d.DashboardHub == nil {
		panic("ws: DashboardHub is nil")
	}
	if d.Logger == nil {
		panic("ws: Logger is nil")
	}

	up := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		// Browser dashboard clients request Sec-WebSocket-Protocol: arx-dashboard; gorilla must echo it in 101.
		Subprotocols: []string{"arx-dashboard"},
		CheckOrigin: func(r *http.Request) bool {
			if dashboardProtoRequested(r) {
				return originAllowed(r, d.DashboardOrigins)
			}
			return r.Header.Get("Origin") == ""
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		dashboardHandshake := dashboardProtoRequested(r)
		if dashboardHandshake {
			// Operator dashboard: JWT + Origin allowlist. TLS is required only when the server runs HTTPS/mTLS.
			if d.MTLSRequired && r.TLS == nil {
				http.Error(w, "tls required", http.StatusForbidden)
				return
			}
		} else {
			// Agent C2 always requires server TLS and a verified client certificate.
			if !d.MTLSRequired {
				http.Error(w, "websocket requires server TLS and client CA bundle", http.StatusServiceUnavailable)
				return
			}
			if r.TLS == nil {
				http.Error(w, "tls required", http.StatusForbidden)
				return
			}
			if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
				http.Error(w, "mutual tls with a verified client certificate is required", http.StatusForbidden)
				return
			}
		}

		var dashPrincipal auth.Principal
		if dashboardHandshake {
			var err error
			dashPrincipal, err = dashboardJWTPrincipal(r, d.DashboardJWT, d.DashboardOrigins)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			d.Logger.Warn("websocket upgrade failed", "err", err, "request_id", r.Header.Get("X-Request-Id"))
			return
		}

		if conn.Subprotocol() == "arx-dashboard" {
			d.Logger.Info("dashboard websocket connected", "request_id", r.Header.Get("X-Request-Id"))
			dc := d.DashboardHub.register(conn, dashPrincipal)
			RunDashboardSession(r, dc, d.Pool, d.C2Hub, d.Logger)
			return
		}

		if len(r.TLS.PeerCertificates) == 0 {
			_ = conn.Close()
			return
		}
		leaf := r.TLS.PeerCertificates[0]
		serial := api.CertSerialDecimal(leaf)
		if serial == "" {
			_ = conn.Close()
			return
		}

		session := d.C2Hub.register(serial, conn)
		d.Logger.Info("agent websocket connected",
			"cert_serial", serial,
			"request_id", r.Header.Get("X-Request-Id"),
		)

		go writePump(session, d.Logger)
		telDeps := d.AgentTelemetry
		if telDeps.Pool == nil {
			telDeps.Pool = d.Pool
		}
		if telDeps.Logger == nil {
			telDeps.Logger = d.Logger
		}
		readPump(r, session, d.Pool, d.DashboardHub, d.Logger, telDeps)
	}
}

func readPump(r *http.Request, s *agentSession, pool *pgxpool.Pool, dash *DashboardHub, logger *slog.Logger, telDeps AgentTelemetryDeps) {
	defer func() {
		_ = s.conn.Close()
		s.hub.unregister(s)
		logger.Info("agent websocket disconnected", "cert_serial", s.serial, "request_id", r.Header.Get("X-Request-Id"))
	}()

	_ = s.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) &&
				!errors.Is(err, websocket.ErrCloseSent) {
				logger.Debug("websocket read ended", "cert_serial", s.serial, "err", err)
			}
			return
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		readCtx, readCancel := context.WithTimeout(r.Context(), 30*time.Second)
		if tryHandleAgentTelemetry(readCtx, s.serial, data, telDeps) {
			readCancel()
			continue
		}
		readCancel()

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		_ = ApplyPackageDeploymentOutcome(ctx, pool, s.serial, data, logger)
		cancel()
		if s.hub.TryDeliverFileTransfer(s.serial, data) {
			continue
		}
		if tryBroadcastAgentUplink(r.Context(), pool, dash, s.serial, data, logger) {
			continue
		}
	}
}

func writePump(s *agentSession, logger *slog.Logger) {
	for msg := range s.send {
		_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
		if err := s.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			logger.Debug("websocket write failed", "cert_serial", s.serial, "err", err)
			_ = s.conn.Close()
			return
		}
	}
}
