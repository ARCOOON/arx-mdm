//go:build linux

package agent

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxAgentBinary   = "/usr/local/bin/arx-agent"
	linuxCertDir       = "/var/lib/arx-agent/certs"
	linuxSystemdUnit   = "/etc/systemd/system/arx-agent.service"
	linuxAgentStateDir = "/var/lib/arx-agent"
)

// ScheduleEnterpriseWipeLinux starts a transient systemd scope (when available) so the wipe
// worker survives stopping the agent service.
func ScheduleEnterpriseWipeLinux(logger *slog.Logger) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("enterprise wipe: resolve executable: %w", err)
	}
	self = filepath.Clean(self)

	bin := linuxAgentBinary
	if _, err := os.Stat(bin); err != nil {
		bin = self
	}

	if path, err := exec.LookPath("systemd-run"); err == nil {
		cmd := exec.Command(path, "--collect", "--unit=arx-enterprise-wipe", bin, "enterprise-wipe-worker")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("enterprise wipe: systemd-run: %w", err)
		}
		if logger != nil {
			logger.Info("enterprise wipe worker launched via systemd-run", "bin", bin)
		}
		return nil
	}

	cmd := exec.Command(bin, "enterprise-wipe-worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("enterprise wipe: start worker: %w", err)
	}
	if logger != nil {
		logger.Info("enterprise wipe worker launched (direct child)", "bin", bin)
	}
	return nil
}

// RunEnterpriseWipeWorker removes the packaged Linux agent, certificates, systemd registration,
// and reboots the host. Must not run inside the primary service process.
func RunEnterpriseWipeWorker(logger *slog.Logger) {
	time.Sleep(2 * time.Second)

	_ = runCmd(logger, "systemctl", "stop", "arx-agent.service")
	_ = os.RemoveAll(linuxCertDir)

	if err := os.Remove(linuxSystemdUnit); err != nil && logger != nil && !os.IsNotExist(err) {
		logger.Warn("enterprise wipe: remove unit file", "err", err.Error())
	}
	_ = runCmd(logger, "systemctl", "daemon-reload")

	if err := os.Remove(linuxAgentBinary); err != nil && logger != nil && !os.IsNotExist(err) {
		logger.Warn("enterprise wipe: remove agent binary", "err", err.Error())
	}

	// Best-effort removal of empty state directory (certs already removed).
	if err := os.Remove(linuxAgentStateDir); err != nil && logger != nil && !os.IsNotExist(err) {
		logger.Debug("enterprise wipe: state dir removal", "err", err.Error())
	}

	unix.Sync()
	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil && logger != nil {
		logger.Error("enterprise wipe: reboot failed", "err", err)
	}
}

func runCmd(logger *slog.Logger, name string, arg ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		if logger != nil {
			logger.Warn("enterprise wipe: binary not found", "name", name)
		}
		return err
	}
	cmd := exec.Command(path, arg...)
	out, err := cmd.CombinedOutput()
	if err != nil && logger != nil {
		logger.Warn("enterprise wipe: command failed", "cmd", name, "err", err.Error(), "output", string(out))
	}
	return err
}
