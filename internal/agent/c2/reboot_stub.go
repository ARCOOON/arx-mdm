//go:build !linux && !windows

package c2

import "fmt"

func executeReboot() (string, error) {
	return "", fmt.Errorf("reboot is not supported on this platform")
}
