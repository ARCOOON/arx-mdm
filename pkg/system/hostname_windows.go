//go:build windows

package system

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSetComputerNameExW = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetComputerNameExW")

// SetHostname updates the computer DNS hostname via SetComputerNameExW (native kernel32).
func SetHostname(name string) error {
	if name == "" {
		return fmt.Errorf("hostname is required")
	}
	if len(name) > 255 {
		return fmt.Errorf("hostname too long")
	}
	u, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r1, _, e1 := procSetComputerNameExW.Call(
		uintptr(windows.ComputerNameDnsHostname),
		uintptr(unsafe.Pointer(u)),
	)
	if r1 == 0 {
		return e1
	}
	return nil
}
