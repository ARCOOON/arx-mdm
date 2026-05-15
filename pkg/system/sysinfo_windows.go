//go:build windows

package system

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func collectSystemInfo() (SystemInfo, error) {
	var out SystemInfo
	var err error

	out.Hostname, err = os.Hostname()
	if err != nil {
		return out, fmt.Errorf("system: hostname: %w", err)
	}
	out.OSFamily = runtime.GOOS

	out.OSVersion, err = windowsOSVersion()
	if err != nil {
		return out, err
	}

	out.TotalRAMBytes, err = windowsTotalRAMBytes()
	if err != nil {
		return out, err
	}

	out.CPUModel, out.CPULogicalCores, err = windowsCPUInfo()
	if err != nil {
		return out, err
	}

	if used, tot, err := windowsMemoryUsedAndTotal(); err == nil && tot > 0 {
		out.MemoryUsedBytes = used
		if out.TotalRAMBytes == 0 {
			out.TotalRAMBytes = tot
		}
	}
	if pct, err := windowsCPUUsagePercent(150 * time.Millisecond); err == nil {
		out.CPUUsagePercent = pct
	}
	return out, nil
}

// windowsMemoryUsedAndTotal uses GlobalMemoryStatusEx (used = total - available physical).
func windowsMemoryUsedAndTotal() (used, total uint64, err error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")
	var msx struct {
		dwLength                uint32
		dwMemoryLoad            uint32
		ullTotalPhys            uint64
		ullAvailPhys            uint64
		ullTotalPageFile        uint64
		ullAvailPageFile        uint64
		ullTotalVirtual         uint64
		ullAvailVirtual         uint64
		ullAvailExtendedVirtual uint64
	}
	msx.dwLength = uint32(unsafe.Sizeof(msx))
	r1, _, e1 := syscall.SyscallN(proc.Addr(), uintptr(unsafe.Pointer(&msx)))
	if r1 == 0 {
		if e1 != 0 {
			return 0, 0, fmt.Errorf("system: GlobalMemoryStatusEx: %w", e1)
		}
		return 0, 0, fmt.Errorf("system: GlobalMemoryStatusEx failed")
	}
	total = msx.ullTotalPhys
	avail := msx.ullAvailPhys
	if total > avail {
		return total - avail, total, nil
	}
	return 0, total, nil
}

func windowsCPUUsagePercent(sampleWait time.Duration) (float64, error) {
	var idle1, kernel1, user1 windows.Filetime
	if err := getSystemTimesProc(&idle1, &kernel1, &user1); err != nil {
		return 0, err
	}
	time.Sleep(sampleWait)
	var idle2, kernel2, user2 windows.Filetime
	if err := getSystemTimesProc(&idle2, &kernel2, &user2); err != nil {
		return 0, err
	}

	idle1n := filetimeToUint64(idle1)
	idle2n := filetimeToUint64(idle2)
	kernel1n := filetimeToUint64(kernel1)
	kernel2n := filetimeToUint64(kernel2)
	user1n := filetimeToUint64(user1)
	user2n := filetimeToUint64(user2)

	idleDelta := float64(idle2n - idle1n)
	kernelDelta := float64(kernel2n - kernel1n)
	userDelta := float64(user2n - user1n)
	totalDelta := kernelDelta + userDelta
	if totalDelta <= 0 {
		return 0, nil
	}
	busy := totalDelta - idleDelta
	if busy < 0 {
		busy = 0
	}
	pct := 100.0 * busy / totalDelta
	if pct > 100 {
		pct = 100
	}
	return pct, nil
}

func filetimeToUint64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes = kernel32.NewProc("GetSystemTimes")
)

func getSystemTimesProc(idle, kernel, user *windows.Filetime) error {
	r1, _, e1 := syscall.SyscallN(procGetSystemTimes.Addr(),
		uintptr(unsafe.Pointer(idle)),
		uintptr(unsafe.Pointer(kernel)),
		uintptr(unsafe.Pointer(user)),
	)
	if r1 == 0 {
		if e1 != 0 {
			return fmt.Errorf("system: GetSystemTimes: %w", e1)
		}
		return fmt.Errorf("system: GetSystemTimes failed")
	}
	return nil
}

func windowsTotalRAMBytes() (uint64, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("GetPhysicallyInstalledSystemMemory")
	var memKB uint64
	r1, _, e1 := syscall.SyscallN(proc.Addr(), uintptr(unsafe.Pointer(&memKB)))
	if r1 == 0 {
		if e1 != 0 {
			return 0, fmt.Errorf("system: GetPhysicallyInstalledSystemMemory: %w", e1)
		}
		return 0, fmt.Errorf("system: GetPhysicallyInstalledSystemMemory failed")
	}
	return memKB * 1024, nil
}

func windowsCPUInfo() (model string, logicalCores int, err error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor`, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return "", 0, fmt.Errorf("system: open CentralProcessor registry key: %w", err)
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(0)
	if err != nil {
		return "", 0, fmt.Errorf("system: enumerate CentralProcessor subkeys: %w", err)
	}
	logicalCores = len(names)
	if logicalCores == 0 {
		return "", 0, fmt.Errorf("system: no CentralProcessor subkeys found")
	}

	sub0, err := registry.OpenKey(k, names[0], registry.QUERY_VALUE)
	if err != nil {
		return "", 0, fmt.Errorf("system: open CPU subkey %q: %w", names[0], err)
	}
	defer sub0.Close()

	model, _, err = sub0.GetStringValue("ProcessorNameString")
	if err != nil {
		return "", 0, fmt.Errorf("system: read ProcessorNameString: %w", err)
	}
	model = sanitizeRegistryString(model)
	return model, logicalCores, nil
}

func windowsOSVersion() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("system: open Windows NT CurrentVersion key: %w", err)
	}
	defer k.Close()

	product, _, err := k.GetStringValue("ProductName")
	if err != nil {
		return "", fmt.Errorf("system: read ProductName: %w", err)
	}
	product = sanitizeRegistryString(product)

	build, _, err := k.GetStringValue("CurrentBuild")
	if err != nil {
		return "", fmt.Errorf("system: read CurrentBuild: %w", err)
	}
	build = sanitizeRegistryString(build)

	display, _, _ := k.GetStringValue("DisplayVersion")
	display = sanitizeRegistryString(display)

	if display != "" {
		return fmt.Sprintf("%s (%s, build %s)", product, display, build), nil
	}
	return fmt.Sprintf("%s (build %s)", product, build), nil
}

func sanitizeRegistryString(s string) string {
	return string([]byte(s)) // strip embedded NULs from Win32 strings
}
