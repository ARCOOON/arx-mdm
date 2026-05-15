// Package cmdloop breaks the agent/ws import cycle: the agent client invokes the
// command read loop registered by the ws package at init time.
package cmdloop

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gorilla/websocket"
)

// LoopFunc reads server downlink WebSocket messages until the connection closes.
type LoopFunc func(ctx context.Context, logger *slog.Logger, conn *websocket.Conn) error

var run LoopFunc

// Register installs the command-loop implementation (called from internal/ws init).
func Register(fn LoopFunc) {
	run = fn
}

// Run executes the registered command loop.
func Run(ctx context.Context, logger *slog.Logger, conn *websocket.Conn) error {
	if run == nil {
		return errors.New("cmdloop: not registered (internal/ws init missing)")
	}
	return run(ctx, logger, conn)
}
