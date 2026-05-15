package ws

import (
	"context"
	"log/slog"

	"github.com/ARCOOON/arx-mdm/internal/agent"
)

// ClientOptions configures the agent-side C2 WebSocket client.
type ClientOptions struct {
	ServerURL string
	CertDir   string
	Logger    *slog.Logger
}

// RunClient maintains a WSS connection to /v1/ws with mTLS, telemetry uplink, and exponential backoff until ctx ends.
func RunClient(ctx context.Context, opts ClientOptions) error {
	return agent.Run(ctx, agent.RunOptions{
		ServerURL: opts.ServerURL,
		CertDir:   opts.CertDir,
		Logger:    opts.Logger,
	})
}
