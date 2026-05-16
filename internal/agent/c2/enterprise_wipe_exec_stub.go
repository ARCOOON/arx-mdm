//go:build !windows && !linux

package c2

import "log/slog"

func ExecuteEnterpriseWipe(logger *slog.Logger) {
	if logger != nil {
		logger.Error("enterprise wipe is only supported on Windows and Linux agents")
	}
}
