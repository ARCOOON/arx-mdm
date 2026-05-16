//go:build linux

package c2

import (
	"fmt"
	"os/exec"
	"time"
)

func executeLockWorkstation() error {
	path, err := exec.LookPath("loginctl")
	if err != nil {
		return fmt.Errorf("lock: loginctl not found: %w", err)
	}
	cmd := exec.Command(path, "lock-sessions")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lock: loginctl lock-sessions: %w", err)
	}
	time.Sleep(100 * time.Millisecond)
	return nil
}
