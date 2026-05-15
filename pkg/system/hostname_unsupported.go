//go:build !linux && !windows

package system

import "fmt"

// SetHostname is not implemented on this platform build.
func SetHostname(name string) error {
	_ = name
	return fmt.Errorf("SetHostname is not supported on this platform")
}
