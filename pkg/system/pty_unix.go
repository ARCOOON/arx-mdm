//go:build unix

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

func spawnPTY(ctx context.Context, cols, rows uint16, argv []string) (*PTYSession, error) {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 25
	}
	if len(argv) == 0 {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		argv = []string{sh, "-i"}
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, fmt.Errorf("system: pty start: %w", err)
	}

	return &PTYSession{
		Stdin:  tty,
		Stdout: tty,
		Resize: func(cols, rows uint16) error {
			if cols < 2 {
				cols = 80
			}
			if rows < 2 {
				rows = 25
			}
			return pty.Setsize(tty, &pty.Winsize{Rows: rows, Cols: cols})
		},
		Close: func() error {
			_ = tty.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return nil
		},
		Wait: func() error {
			return cmd.Wait()
		},
	}, nil
}
