//go:build linux

package c2

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func executeReboot() (string, error) {
	unix.Sync()
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
		return "", fmt.Errorf("reboot: %w", err)
	}
	return "reboot initiated", nil
}
