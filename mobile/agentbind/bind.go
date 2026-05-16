// Package agentbind is gomobile-bindable agent telemetry and C2 for the Android shell app.
package agentbind

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/agent"
	"github.com/ARCOOON/arx-mdm/internal/agent/telemetry"
)

const (
	c2Offline     = "offline"
	c2Connecting = "connecting"
	c2Online    = "online"
)

var (
	agentMu     sync.Mutex
	agentCancel context.CancelFunc

	configMu sync.Mutex
	lastServerURL string
	lastCertDir   string

	stateMu                sync.RWMutex
	c2Status               = c2Offline
	lastTelemetryUnixMilli int64

	kickMu          sync.Mutex
	telemetryKickCh chan struct{}
)

// CollectTelemetryJSON returns a JSON object with host metrics from gopsutil.
func CollectTelemetryJSON() (string, error) {
	snap, err := telemetry.Collect()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Version returns the bind library version string.
func Version() string {
	return "0.1.0"
}

func setC2Status(s string) {
	stateMu.Lock()
	c2Status = s
	stateMu.Unlock()
}

func markTelemetrySent() {
	stateMu.Lock()
	lastTelemetryUnixMilli = time.Now().UnixMilli()
	stateMu.Unlock()
}

// C2Status returns the WebSocket session status: "offline", "connecting", or "online".
func C2Status() string {
	stateMu.RLock()
	s := c2Status
	stateMu.RUnlock()
	if s == "" {
		return c2Offline
	}
	return s
}

// LastTelemetryUnixMilli is the wall-clock time of the last successful C2 telemetry send, or 0 if none yet.
func LastTelemetryUnixMilli() int64 {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return lastTelemetryUnixMilli
}

// SyncTelemetryNow requests an immediate telemetry push over the active WebSocket (non-blocking, coalesced).
func SyncTelemetryNow() {
	kickMu.Lock()
	ch := telemetryKickCh
	kickMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// ForceReconnect drops the agent run loop and starts it again with the last successful StartAgent URL and cert dir.
func ForceReconnect() {
	configMu.Lock()
	u, d := lastServerURL, lastCertDir
	configMu.Unlock()
	if strings.TrimSpace(u) == "" || strings.TrimSpace(d) == "" {
		return
	}
	StopAgent()
	time.Sleep(150 * time.Millisecond)
	StartAgent(u, d)
}

// StartAgent launches the mutual-TLS WebSocket C2 client and telemetry uplink in a background goroutine.
// certDir must contain client.key, client.crt, and root_ca.pem (see internal/agent.ClientMaterialPaths).
// If an agent is already running, it is stopped and replaced.
func StartAgent(serverURL, certDir string) {
	serverURL = strings.TrimSpace(serverURL)
	certDir = strings.TrimSpace(certDir)
	if serverURL == "" || certDir == "" {
		return
	}
	StopAgent()

	configMu.Lock()
	lastServerURL = serverURL
	lastCertDir = certDir
	configMu.Unlock()

	ch := make(chan struct{}, 1)
	kickMu.Lock()
	telemetryKickCh = ch
	kickMu.Unlock()

	setC2Status(c2Connecting)

	ctx, cancel := context.WithCancel(context.Background())
	agentMu.Lock()
	agentCancel = cancel
	agentMu.Unlock()

	go func(kick <-chan struct{}) {
		defer setC2Status(c2Offline)
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		_ = agent.Run(ctx, agent.RunOptions{
			ServerURL:         serverURL,
			CertDir:           certDir,
			Logger:            logger,
			TelemetryInterval: 60 * time.Second,
			TelemetryKick:     kick,
			OnConnecting:      func() { setC2Status(c2Connecting) },
			OnConnected:       func() { setC2Status(c2Online) },
			OnDisconnected:    func() { setC2Status(c2Connecting) },
			OnTelemetrySent:   markTelemetrySent,
		})
	}(ch)
}

// StopAgent cancels the background agent started by StartAgent.
func StopAgent() {
	agentMu.Lock()
	if agentCancel != nil {
		agentCancel()
		agentCancel = nil
	}
	agentMu.Unlock()

	kickMu.Lock()
	telemetryKickCh = nil
	kickMu.Unlock()

	setC2Status(c2Offline)
}
