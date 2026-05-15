//go:build windows

package agent

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// InitiateHostShutdown requests an immediate system shutdown using ExitWindowsEx after enabling
// SeShutdownPrivilege on the current process token.
func InitiateHostShutdown() error {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return fmt.Errorf("agent: open process token: %w", err)
	}
	defer token.Close()

	privName, err := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return fmt.Errorf("agent: privilege name: %w", err)
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, privName, &luid); err != nil {
		return fmt.Errorf("agent: lookup SeShutdownPrivilege: %w", err)
	}

	var tp windows.Tokenprivileges
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED

	if err := windows.AdjustTokenPrivileges(token, false, &tp, uint32(unsafe.Sizeof(tp)), nil, nil); err != nil {
		return fmt.Errorf("agent: adjust token privileges: %w", err)
	}

	flags := uint32(windows.EWX_SHUTDOWN | windows.EWX_FORCE)
	reason := uint32(windows.SHTDN_REASON_MAJOR_OTHER | windows.SHTDN_REASON_FLAG_PLANNED)
	if err := windows.ExitWindowsEx(flags, reason); err != nil {
		return fmt.Errorf("agent: ExitWindowsEx: %w", err)
	}
	return nil
}
