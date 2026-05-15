package ws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"arx-mdm/internal/agent"

	"github.com/gorilla/websocket"
)

// ClientOptions configures the agent-side C2 WebSocket client.
type ClientOptions struct {
	ServerURL string
	CertDir   string
	Logger    *slog.Logger
}

const (
	wsHandshakeTimeout = 45 * time.Second
	initialBackoff     = time.Second
	maxBackoff         = 60 * time.Second
)

// RunClient maintains a WSS connection to /v1/ws with mTLS, reconnecting with exponential backoff until ctx ends.
func RunClient(ctx context.Context, opts ClientOptions) error {
	if opts.Logger == nil {
		return errors.New("ws: logger is required")
	}
	server := strings.TrimSpace(opts.ServerURL)
	if server == "" {
		return errors.New("ws: ServerURL is required")
	}
	certDir := strings.TrimSpace(opts.CertDir)
	if certDir == "" {
		certDir = agent.DefaultCertDir()
	}

	wsURL, err := joinWebSocketURL(server)
	if err != nil {
		return err
	}

	tlsCfg, err := agent.MTLSClientConfig(server, certDir)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  tlsCfg,
		HandshakeTimeout: wsHandshakeTimeout,
	}

	backoff := initialBackoff
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
			if wait(ctx, backoff) != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				return ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		backoff = initialBackoff
		opts.Logger.Info("websocket connected", "url", wsURL)

		readErr := readCommandsUntilDead(ctx, opts.Logger, conn)
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
			backoff = nextBackoff(backoff)
		}
	}
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	n := cur * 2
	if n > maxBackoff {
		return maxBackoff
	}
	return n
}

func joinWebSocketURL(serverBase string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverBase))
	if err != nil {
		return "", fmt.Errorf("ws: parse ServerURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ws: ServerURL must include scheme and host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("ws: unsupported URL scheme %q (use http or https)", u.Scheme)
	}
	u = u.JoinPath("v1", "ws")
	return u.String(), nil
}
