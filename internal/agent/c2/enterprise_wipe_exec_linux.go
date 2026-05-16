//go:build linux

package c2

import (
	"log/slog"

	"github.com/ARCOOON/arx-mdm/internal/agent"
)

func ExecuteEnterpriseWipe(logger *slog.Logger) {
	if err := agent.ScheduleEnterpriseWipeLinux(logger); err != nil && logger != nil {
		logger.Error("enterprise wipe schedule failed", "err", err)
	}
}
