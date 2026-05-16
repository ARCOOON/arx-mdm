//go:build windows

package c2

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func executeReboot() (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return "", fmt.Errorf("reboot: open process token: %w", err)
	}
	defer token.Close()

	privName, err := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return "", fmt.Errorf("reboot: privilege name: %w", err)
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, privName, &luid); err != nil {
		return "", fmt.Errorf("reboot: lookup SeShutdownPrivilege: %w", err)
	}
	var tp windows.Tokenprivileges
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED
	if err := windows.AdjustTokenPrivileges(token, false, &tp, uint32(unsafe.Sizeof(tp)), nil, nil); err != nil {
		return "", fmt.Errorf("reboot: adjust token privileges: %w", err)
	}

	flags := uint32(windows.EWX_REBOOT | windows.EWX_FORCE)
	reason := uint32(windows.SHTDN_REASON_MAJOR_OTHER | windows.SHTDN_REASON_FLAG_PLANNED)
	if err := windows.ExitWindowsEx(flags, reason); err != nil {
		return "", fmt.Errorf("reboot: ExitWindowsEx: %w", err)
	}
	return "reboot initiated", nil
}
