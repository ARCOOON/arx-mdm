//go:build linux

package system

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func collectSystemInfo() (SystemInfo, error) {
	var out SystemInfo
	var err error

	out.Hostname, err = os.Hostname()
	if err != nil {
		return out, fmt.Errorf("system: hostname: %w", err)
	}
	out.OSFamily = runtime.GOOS

	out.OSVersion, err = linuxOSVersion()
	if err != nil {
		return out, err
	}

	total, avail, err := linuxMemTotalAndAvailableBytes()
	if err != nil {
		return out, err
	}
	out.TotalRAMBytes = total
	if avail > 0 && total >= avail {
		out.MemoryUsedBytes = total - avail
	}

	out.CPUModel, out.CPULogicalCores, err = linuxCPUInfo()
	if err != nil {
		return out, err
	}

	if pct, err := linuxCPUUsagePercent(100 * time.Millisecond); err == nil {
		out.CPUUsagePercent = pct
	}
	return out, nil
}

func linuxOSVersion() (string, error) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("system: read /etc/os-release: %w", err)
	}
	lines := parseEnvFile(b)
	if v := strings.TrimSpace(lines["PRETTY_NAME"]); v != "" {
		return v, nil
	}
	name := strings.TrimSpace(lines["NAME"])
	ver := strings.TrimSpace(lines["VERSION"])
	if name != "" && ver != "" {
		return name + " " + ver, nil
	}
	if name != "" {
		return name, nil
	}
	return "linux", nil
}

func parseEnvFile(data []byte) map[string]string {
	m := make(map[string]string)
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)
		m[key] = val
	}
	return m
}

func linuxMemTotalAndAvailableBytes() (total uint64, avail uint64, err error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("system: read /proc/meminfo: %w", err)
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	var totalKB, availKB uint64
	for s.Scan() {
		line := s.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[2] != "kB" {
			continue
		}
		kb, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = kb
		case "MemAvailable:":
			availKB = kb
		}
	}
	if err := s.Err(); err != nil {
		return 0, 0, fmt.Errorf("system: scan /proc/meminfo: %w", err)
	}
	if totalKB == 0 {
		return 0, 0, fmt.Errorf("system: MemTotal not found in /proc/meminfo")
	}
	return totalKB * 1024, availKB * 1024, nil
}

func linuxCPUInfo() (model string, logicalCores int, err error) {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", 0, fmt.Errorf("system: read /proc/cpuinfo: %w", err)
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		line := s.Text()
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "processor" {
				if _, err := strconv.Atoi(val); err == nil {
					logicalCores++
				}
			}
			if model == "" && strings.EqualFold(key, "Hardware") {
				model = val
			}
		}
		low := strings.ToLower(line)
		if model == "" && strings.HasPrefix(low, "model name") {
			if _, after, ok := strings.Cut(line, ":"); ok {
				model = strings.TrimSpace(after)
			}
		}
		if model == "" && strings.HasPrefix(strings.TrimSpace(line), "Hardware") {
			if _, after, ok := strings.Cut(line, ":"); ok {
				model = strings.TrimSpace(after)
			}
		}
	}
	if err := s.Err(); err != nil {
		return "", 0, fmt.Errorf("system: scan /proc/cpuinfo: %w", err)
	}
	if model == "" {
		return "", logicalCores, fmt.Errorf("system: model name not found in /proc/cpuinfo")
	}
	if logicalCores == 0 {
		return "", 0, fmt.Errorf("system: no logical processors found in /proc/cpuinfo")
	}
	return model, logicalCores, nil
}

// linuxCPUUsagePercent estimates CPU utilization from /proc/stat deltas over sampleWait.
func linuxCPUUsagePercent(sampleWait time.Duration) (float64, error) {
	total1, idle1, err := readProcStatAggregate()
	if err != nil {
		return 0, err
	}
	time.Sleep(sampleWait)
	total2, idle2, err := readProcStatAggregate()
	if err != nil {
		return 0, err
	}
	dTotal := int64(total2 - total1)
	dIdle := int64(idle2 - idle1)
	if dTotal <= 0 {
		return 0, nil
	}
	busy := dTotal - dIdle
	if busy < 0 {
		busy = 0
	}
	pct := 100.0 * float64(busy) / float64(dTotal)
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct, nil
}

// readProcStatAggregate returns cumulative jiffies for the aggregate "cpu" line and its idle counter.
func readProcStatAggregate() (total, idle uint64, err error) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("system: read /proc/stat: %w", err)
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	if !s.Scan() {
		return 0, 0, fmt.Errorf("system: empty /proc/stat")
	}
	line := s.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return 0, 0, fmt.Errorf("system: unexpected first /proc/stat line: %q", line)
	}
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return 0, 0, fmt.Errorf("system: short /proc/stat cpu line: %q", line)
	}
	var sum uint64
	var idleVal uint64
	for i := 1; i < len(fields); i++ {
		v, perr := strconv.ParseUint(fields[i], 10, 64)
		if perr != nil {
			return 0, 0, fmt.Errorf("system: parse /proc/stat field %q: %w", fields[i], perr)
		}
		sum += v
		if i == 4 {
			idleVal = v
		}
	}
	return sum, idleVal, s.Err()
}
