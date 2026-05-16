//go:build !windows && !(linux && !android) && !android

package c2

import "fmt"

func platformApplyQuarantine(enabled bool, hosts []string, ports []uint16) (string, error) {
	_ = enabled
	_ = hosts
	_ = ports
	return "", fmt.Errorf("network quarantine is not implemented on this operating system")
}
