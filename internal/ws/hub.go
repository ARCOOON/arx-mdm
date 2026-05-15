package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	outboundQueueDepth = 32
	dispatchSendWait   = 10 * time.Second
)

// Hub tracks active agent WebSocket sessions keyed by TLS leaf certificate serial number in decimal,
// matching api.CertSerialDecimal and assets.cert_serial.
//
// Phase 10 adds fs_listdir / net_list / hostname_set fan-out and REST file streaming via TryDeliverFileTransfer.
//
// Advanced UEM (Phase 7): dashboard commands such as registry_* and pty_* are forwarded to the
// connected agent with DispatchJSON / DispatchJSONByHumanID (see dash_session.go). Agent replies
// (registry_result, pty_output, …) are broadcast to dashboards from the agent read path via
// tryBroadcastAgentUplink in agent_uplink.go.
//
// Phase 14: successful dashboard C&C dispatches are recorded in audit_logs from RunDashboardSession
// (not from this struct directly).
//
// Dispatch to a connected agent by certificate serial:
//
//	hub.DispatchJSON(certSerial, map[string]string{"action": "shutdown"})
//
// Or resolve the serial from the database by ARX human_id (e.g. arx-c-1):
//
//	hub.DispatchJSONByHumanID(ctx, pool, "arx-c-1", map[string]string{"action": "shutdown"})
type Hub struct {
	mu       sync.RWMutex
	bySerial map[string]*agentSession

	// REST file proxy waiters (agent → server chunked download / upload result).
	fsMu   sync.Mutex
	fsDown map[string]*fsDownloadWaiter
	fsUp   map[string]chan api.C2FileUploadResult
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{
		bySerial: make(map[string]*agentSession),
	}
}

type agentSession struct {
	hub    *Hub
	serial string
	conn   *websocket.Conn
	send   chan []byte
}

func (h *Hub) register(serial string, conn *websocket.Conn) *agentSession {
	s := &agentSession{
		hub:    h,
		serial: serial,
		conn:   conn,
		send:   make(chan []byte, outboundQueueDepth),
	}

	h.mu.Lock()
	prev := h.bySerial[serial]
	h.bySerial[serial] = s
	h.mu.Unlock()

	if prev != nil {
		_ = prev.conn.Close()
	}
	return s
}

func (h *Hub) unregister(s *agentSession) {
	if s == nil {
		return
	}
	h.mu.Lock()
	if cur, ok := h.bySerial[s.serial]; ok && cur == s {
		delete(h.bySerial, s.serial)
	}
	h.mu.Unlock()
	close(s.send)
}

// ConnectedCertSerials returns a snapshot of certificate serials that currently have an active session.
func (h *Hub) ConnectedCertSerials() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.bySerial))
	for serial := range h.bySerial {
		out = append(out, serial)
	}
	return out
}

// DispatchJSON sends a JSON-serialized payload to the agent session for the given certificate serial.
// It returns false if no session is registered for that serial or if the outbound queue is blocked too long.
func (h *Hub) DispatchJSON(certSerial string, payload any) bool {
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return false
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return h.dispatchRaw(certSerial, data)
}

func (h *Hub) dispatchRaw(certSerial string, data []byte) bool {
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return false
	}
	h.mu.RLock()
	s := h.bySerial[certSerial]
	h.mu.RUnlock()
	if s == nil {
		return false
	}
	timer := time.NewTimer(dispatchSendWait)
	defer timer.Stop()
	select {
	case s.send <- data:
		return true
	case <-timer.C:
		return false
	}
}

// DispatchJSONByHumanID resolves assets.cert_serial for humanID and dispatches if that agent is connected.
func (h *Hub) DispatchJSONByHumanID(ctx context.Context, pool *pgxpool.Pool, humanID string, payload any) error {
	if pool == nil {
		return errors.New("ws: pool is required")
	}
	humanID = strings.TrimSpace(humanID)
	if humanID == "" {
		return errors.New("ws: humanID is required")
	}
	var certSerial string
	err := pool.QueryRow(ctx, `SELECT cert_serial FROM assets WHERE human_id = $1 LIMIT 1`, humanID).Scan(&certSerial)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ws: no asset for human_id %q", humanID)
		}
		return fmt.Errorf("ws: lookup cert_serial: %w", err)
	}
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return fmt.Errorf("ws: asset %q has no cert_serial", humanID)
	}
	if !h.DispatchJSON(certSerial, payload) {
		return fmt.Errorf("ws: agent not connected for human_id %q (cert_serial %q)", humanID, certSerial)
	}
	return nil
}
