//go:build !windows

package agent

import (
	"fmt"
	"log/slog"
)

// RunUninstall is not available on this platform; use scripts/uninstall_agent.sh on Linux.
func RunUninstall(logger *slog.Logger, _ UninstallOptions) error {
	if logger != nil {
		logger.Info("uninstall skipped on this platform (no in-process destructive steps); use Linux uninstall script as root")
	}
	return fmt.Errorf("%w; on Linux run: sudo bash scripts/uninstall_agent.sh", ErrUninstallPlatform)
}
