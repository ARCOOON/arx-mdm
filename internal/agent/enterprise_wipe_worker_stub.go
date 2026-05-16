//go:build !windows && !linux

package agent

import "log/slog"

// RunEnterpriseWipeWorker is a no-op on platforms without a packaged enterprise agent.
func RunEnterpriseWipeWorker(logger *slog.Logger) {
	if logger != nil {
		logger.Error("enterprise wipe worker is not supported on this platform")
	}
}
