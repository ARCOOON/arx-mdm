// Package system collects native host facts for the ARX MDM agent (no shell wrappers).
package system

// SystemInfo is a platform-neutral snapshot used for telemetry payloads.
type SystemInfo struct {
	Hostname        string  `json:"hostname"`
	OSFamily        string  `json:"os_family"`
	OSVersion       string  `json:"os_version"`
	TotalRAMBytes   uint64  `json:"total_ram_bytes"`
	CPUModel        string  `json:"cpu_model"`
	CPULogicalCores int     `json:"cpu_logical_cores"`
	CPUUsagePercent float64 `json:"cpu_usage_percent,omitempty"`
	MemoryUsedBytes uint64  `json:"memory_used_bytes,omitempty"`
}

// CollectSystemInfo gathers hostname, OS identity, memory, and CPU details using native APIs
// or direct file reads. On unsupported GOOS it returns a structured error.
func CollectSystemInfo() (SystemInfo, error) {
	return collectSystemInfo()
}
