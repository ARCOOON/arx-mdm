package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/agent/cmdloop"
	"github.com/ARCOOON/arx-mdm/internal/agent/telemetry"
	"github.com/ARCOOON/arx-mdm/internal/api"
	"github.com/ARCOOON/arx-mdm/pkg/packagemanager"

	"github.com/gorilla/websocket"
)

const (
	agentMsgTelemetry   = "telemetry"
	wsHandshakeTimeout  = 45 * time.Second
	wsInitialBackoff    = time.Second
	wsMaxBackoff        = 60 * time.Second
	defaultTelemetryGap = 60 * time.Second
)

// RunOptions configures the agent C2 WebSocket client and periodic telemetry uplink.
type RunOptions struct {
	ServerURL         string
	CertDir           string
	Logger            *slog.Logger
	TelemetryInterval time.Duration
}

// Run maintains a mutual-TLS WebSocket to /v1/ws, sends telemetry every TelemetryInterval,
// and reconnects with exponential backoff until ctx is canceled.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.Logger == nil {
		return errors.New("agent: logger is required")
	}
	server := strings.TrimSpace(opts.ServerURL)
	if server == "" {
		return errors.New("agent: ServerURL is required")
	}
	certDir := strings.TrimSpace(opts.CertDir)
	if certDir == "" {
		certDir = defaultCertDir
	}
	interval := opts.TelemetryInterval
	if interval <= 0 {
		interval = defaultTelemetryGap
	}

	wsURL, err := joinWebSocketURL(server)
	if err != nil {
		return err
	}
	tlsCfg, err := MTLSClientConfig(server, certDir)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  tlsCfg,
		HandshakeTimeout: wsHandshakeTimeout,
	}

	backoff := wsInitialBackoff
	for {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		}

		header := http.Header{}
		header.Set("User-Agent", "arx-mdm-agent-c2")

		conn, resp, dialErr := dialer.DialContext(ctx, wsURL, header)
		if dialErr != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			opts.Logger.Warn("websocket dial failed", "err", dialErr, "backoff", backoff.String())
			if waitBackoff(ctx, backoff) != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				return ctx.Err()
			}
			backoff = nextWSBackoff(backoff)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		backoff = wsInitialBackoff
		opts.Logger.Info("websocket connected", "url", wsURL)

		sessCtx, sessCancel := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			runTelemetryLoop(sessCtx, opts.Logger, conn, interval)
		}()

		readErr := cmdloop.Run(sessCtx, opts.Logger, conn)
		sessCancel()
		wg.Wait()
		_ = conn.Close()

		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		}
		if readErr != nil {
			opts.Logger.Warn("websocket session ended", "err", readErr)
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-time.After(backoff):
			backoff = nextWSBackoff(backoff)
		}
	}
}

func runTelemetryLoop(ctx context.Context, logger *slog.Logger, conn *websocket.Conn, interval time.Duration) {
	send := func() {
		msg, err := buildTelemetryWireMessage()
		if err != nil {
			logger.Warn("telemetry collect failed", "err", err)
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			logger.Warn("telemetry websocket send failed", "err", err)
			_ = conn.Close()
			return
		}
		logger.Debug("telemetry sent over websocket")
	}

	send()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func buildTelemetryWireMessage() ([]byte, error) {
	snap, err := telemetry.Collect()
	if err != nil {
		return nil, err
	}
	inv, _ := packagemanager.ListInstalled()
	sw := make([]api.TelemetryInstalledApp, 0, len(inv))
	for _, a := range inv {
		sw = append(sw, api.TelemetryInstalledApp{
			Name: a.Name, Version: a.Version, Source: a.Source, ID: a.ID,
		})
	}
	payload := api.TelemetryPayload{
		Hostname:          snap.Hostname,
		OSFamily:          snap.OSFamily,
		OSVersion:         snap.OSVersion,
		TotalRAMBytes:     snap.TotalRAMBytes,
		CPUModel:          snap.CPUModel,
		CPULogicalCores:   snap.CPULogicalCores,
		CPUUsagePercent:   snap.CPUUsagePercent,
		MemoryUsedBytes:   snap.UsedRAMBytes,
		InstalledSoftware: sw,
	}
	wire := struct {
		Type              string  `json:"type"`
		UptimeSeconds     uint64  `json:"uptime_seconds,omitempty"`
		RootDiskTotalBytes uint64 `json:"root_disk_total_bytes,omitempty"`
		RootDiskFreeBytes  uint64 `json:"root_disk_free_bytes,omitempty"`
		RootDiskUsedBytes  uint64 `json:"root_disk_used_bytes,omitempty"`
		api.TelemetryPayload
	}{
		Type:               agentMsgTelemetry,
		UptimeSeconds:      snap.UptimeSeconds,
		RootDiskTotalBytes: snap.RootDiskTotalBytes,
		RootDiskFreeBytes:  snap.RootDiskFreeBytes,
		RootDiskUsedBytes:  snap.RootDiskUsedBytes,
		TelemetryPayload:   payload,
	}
	return json.Marshal(wire)
}

func joinWebSocketURL(serverBase string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverBase))
	if err != nil {
		return "", fmt.Errorf("agent: parse ServerURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("agent: ServerURL must include scheme and host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("agent: unsupported URL scheme %q (use http or https)", u.Scheme)
	}
	u = u.JoinPath("v1", "ws")
	return u.String(), nil
}

func waitBackoff(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func nextWSBackoff(cur time.Duration) time.Duration {
	n := cur * 2
	if n > wsMaxBackoff {
		return wsMaxBackoff
	}
	return n
}
