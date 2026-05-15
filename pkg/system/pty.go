package system

import (
	"context"
	"io"
)

// PTYSession is the host side of a pseudo-terminal session (combined stdin/stdout on the master).
type PTYSession struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Resize func(cols, rows uint16) error
	Close  func() error
	Wait   func() error
}

// SpawnPTY starts an interactive shell (or argv) attached to a new PTY.
// cols and rows are terminal dimensions (minimum 2).
func SpawnPTY(ctx context.Context, cols, rows uint16, argv []string) (*PTYSession, error) {
	return spawnPTY(ctx, cols, rows, argv)
}
