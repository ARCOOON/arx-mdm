package ws

import (
	"sync"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/gorilla/websocket"
)

const dashOutboundQueueDepth = 64

// DashboardHub fans out JSON messages to operator dashboard WebSocket clients.
type DashboardHub struct {
	mu      sync.RWMutex
	clients map[*dashboardClient]struct{}
}

type dashboardClient struct {
	hub       *DashboardHub
	conn      *websocket.Conn
	send      chan []byte
	principal auth.Principal
}

// NewDashboardHub returns an empty dashboard fan-out hub.
func NewDashboardHub() *DashboardHub {
	return &DashboardHub{
		clients: make(map[*dashboardClient]struct{}),
	}
}

func (h *DashboardHub) register(conn *websocket.Conn, p auth.Principal) *dashboardClient {
	c := &dashboardClient{
		hub:       h,
		conn:      conn,
		send:      make(chan []byte, dashOutboundQueueDepth),
		principal: p,
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *DashboardHub) unregister(c *dashboardClient) {
	if c == nil {
		return
	}
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
}

// Broadcast sends a copy of data to every connected dashboard client. Slow clients may drop messages.
func (h *DashboardHub) Broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		buf := append([]byte(nil), data...)
		select {
		case c.send <- buf:
		default:
		}
	}
}
