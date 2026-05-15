//go:build !linux && !windows

package system

import (
	"fmt"
	"runtime"
)

func collectSystemInfo() (SystemInfo, error) {
	return SystemInfo{}, fmt.Errorf("system: CollectSystemInfo is not implemented on GOOS=%q", runtime.GOOS)
}
