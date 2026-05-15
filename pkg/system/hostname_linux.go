//go:build linux

package system

import (
	"fmt"
	"syscall"
)

// SetHostname sets the kernel hostname (requires appropriate privileges).
func SetHostname(name string) error {
	if name == "" {
		return fmt.Errorf("hostname is required")
	}
	b := []byte(name)
	if len(b) > 255 {
		return fmt.Errorf("hostname too long")
	}
	return syscall.Sethostname(b)
}
