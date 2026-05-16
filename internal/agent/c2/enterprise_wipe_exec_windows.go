//go:build windows

package c2

import (
	"log/slog"

	"github.com/ARCOOON/arx-mdm/internal/agent"
)

func ExecuteEnterpriseWipe(logger *slog.Logger) {
	if err := agent.ScheduleEnterpriseWipeWindows(logger); err != nil && logger != nil {
		logger.Error("enterprise wipe schedule failed", "err", err)
	}
}
