//go:build !windows && !linux

package c2

import (
	"fmt"
	"runtime"
)

func executeLockWorkstation() error {
	return fmt.Errorf("remote lock is not supported on GOOS=%s", runtime.GOOS)
}
