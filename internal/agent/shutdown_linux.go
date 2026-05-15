//go:build linux

package agent

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// InitiateHostShutdown requests an immediate system power-off using the native reboot(2) interface.
func InitiateHostShutdown() error {
	unix.Sync()
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		return fmt.Errorf("agent: reboot power_off: %w", err)
	}
	return nil
}
