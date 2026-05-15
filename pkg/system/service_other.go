//go:build !windows

package system

import (
	"fmt"
	"runtime"
)

// InWindowsService always returns false on non-Windows platforms.
func InWindowsService() (bool, error) {
	return false, nil
}

// InstallWindowsAgentService is only implemented on Windows.
func InstallWindowsAgentService(_ WindowsAgentInstallOptions) error {
	return fmt.Errorf("system: agent service install is only supported on Windows (GOOS=%s)", runtime.GOOS)
}
