// Package telemetry collects cross-platform host metrics for the ARX MDM agent.
package telemetry

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// Snapshot is a platform-neutral telemetry sample (Windows and Linux).
type Snapshot struct {
	Hostname           string
	OSFamily           string
	OSVersion          string
	UptimeSeconds      uint64
	TotalRAMBytes      uint64
	UsedRAMBytes       uint64
	FreeRAMBytes       uint64
	CPUUsagePercent    float64
	RootDiskTotalBytes uint64
	RootDiskUsedBytes  uint64
	RootDiskFreeBytes  uint64
	CPULogicalCores    int
	CPUModel           string
}

// Collect gathers CPU, memory, root disk, OS version, and uptime using gopsutil.
func Collect() (Snapshot, error) {
	var out Snapshot

	hostInfo, err := host.Info()
	if err != nil {
		return out, fmt.Errorf("telemetry: host info: %w", err)
	}
	out.Hostname = hostInfo.Hostname
	if out.Hostname == "" {
		out.Hostname, _ = os.Hostname()
	}
	out.OSFamily = runtime.GOOS
	out.OSVersion = hostInfo.Platform
	if hostInfo.PlatformVersion != "" {
		if out.OSVersion != "" {
			out.OSVersion += " "
		}
		out.OSVersion += hostInfo.PlatformVersion
	}
	if hostInfo.KernelVersion != "" {
		if out.OSVersion != "" {
			out.OSVersion += " (kernel "
			out.OSVersion += hostInfo.KernelVersion
			out.OSVersion += ")"
		} else {
			out.OSVersion = hostInfo.KernelVersion
		}
	}
	if out.OSVersion == "" {
		out.OSVersion = hostInfo.OS
	}
	out.UptimeSeconds = hostInfo.Uptime

	vm, err := mem.VirtualMemory()
	if err != nil {
		return out, fmt.Errorf("telemetry: memory: %w", err)
	}
	out.TotalRAMBytes = vm.Total
	out.UsedRAMBytes = vm.Used
	out.FreeRAMBytes = vm.Free

	root := rootMountPath()
	du, err := disk.Usage(root)
	if err != nil {
		return out, fmt.Errorf("telemetry: disk %s: %w", root, err)
	}
	out.RootDiskTotalBytes = du.Total
	out.RootDiskUsedBytes = du.Used
	out.RootDiskFreeBytes = du.Free

	cores, err := cpu.Counts(true)
	if err == nil && cores > 0 {
		out.CPULogicalCores = cores
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		out.CPUModel = infos[0].ModelName
	}

	// Short sample for a stable utilization percentage.
	if pct, err := cpu.Percent(500*time.Millisecond, false); err == nil && len(pct) > 0 {
		out.CPUUsagePercent = pct[0]
		if out.CPUUsagePercent > 100 {
			out.CPUUsagePercent = 100
		}
		if out.CPUUsagePercent < 0 {
			out.CPUUsagePercent = 0
		}
	}

	return out, nil
}

func rootMountPath() string {
	switch runtime.GOOS {
	case "windows":
		drive := strings.TrimSpace(os.Getenv("SystemDrive"))
		if drive == "" {
			drive = "C:"
		}
		if !strings.HasSuffix(drive, `\`) && !strings.HasSuffix(drive, "/") {
			drive += `\`
		}
		return drive
	default:
		return "/"
	}
}
