//go:build windows

package c2

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func executeLockWorkstation() error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	lock := user32.NewProc("LockWorkStation")
	r1, _, _ := lock.Call()
	if r1 == 0 {
		return fmt.Errorf("LockWorkStation failed")
	}
	return nil
}
