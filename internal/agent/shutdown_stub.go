//go:build !linux && !windows

package agent

import "fmt"

// InitiateHostShutdown is not available on this platform build.
func InitiateHostShutdown() error {
	return fmt.Errorf("agent: host shutdown is not implemented on this operating system")
}
