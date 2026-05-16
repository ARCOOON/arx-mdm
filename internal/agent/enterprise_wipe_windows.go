//go:build windows

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/ARCOOON/arx-mdm/pkg/system"

	"golang.org/x/sys/windows"
)

const moveFileDelayUntilReboot = 0x4

// ScheduleEnterpriseWipeWindows copies the agent binary and starts a detached wipe worker
// so the production service can be stopped and enrollment material removed safely.
func ScheduleEnterpriseWipeWindows(logger *slog.Logger) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("enterprise wipe: resolve executable: %w", err)
	}
	self = filepath.Clean(self)

	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("arx-enterprise-wipe-%d.exe", os.Getpid()))
	if err := copyExecutableFile(self, tmp); err != nil {
		return fmt.Errorf("enterprise wipe: stage worker: %w", err)
	}

	cmd := exec.Command(tmp, "enterprise-wipe-worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000200 | 0x08000000, // CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("enterprise wipe: start worker: %w", err)
	}
	if logger != nil {
		logger.Info("enterprise wipe worker launched", "worker", tmp)
	}
	return nil
}

func copyExecutableFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// RunEnterpriseWipeWorker executes a destructive local wipe; intended only for the
// detached worker process started by ScheduleEnterpriseWipeWindows.
func RunEnterpriseWipeWorker(logger *slog.Logger) {
	time.Sleep(2 * time.Second)

	var certDir, installExe string
	if meta, err := system.ReadAgentInstallMetadata(); err == nil {
		certDir = meta.CertDir
		installExe = meta.InstallExe
	} else if logger != nil {
		logger.Warn("enterprise wipe: registry metadata missing, falling back to SCM service path", "err", err.Error())
	}
	if certDir == "" || installExe == "" {
		if exe, err := system.QueryAgentServiceExePath(); err == nil {
			if installExe == "" {
				installExe = exe
			}
			if certDir == "" {
				certDir = filepath.Join(filepath.Dir(exe), "certs")
			}
		} else if logger != nil {
			logger.Error("enterprise wipe: could not resolve install paths", "err", err.Error())
		}
	}

	if certDir != "" {
		if err := RunUninstall(logger, UninstallOptions{CertDir: certDir}); err != nil && logger != nil {
			logger.Error("enterprise wipe: uninstall step failed", "err", err)
		}
	} else if logger != nil {
		logger.Error("enterprise wipe: skipping uninstall (no certificate directory resolved)")
	}

	if installExe != "" {
		if err := scheduleDeleteFileOnNextBoot(installExe); err != nil && logger != nil {
			logger.Warn("enterprise wipe: schedule install exe deletion failed", "path", installExe, "err", err)
		}
	}

	self, err := os.Executable()
	if err == nil {
		_ = scheduleDeleteFileOnNextBoot(filepath.Clean(self))
	}

	if err := rebootWindowsForWipe(); err != nil && logger != nil {
		logger.Error("enterprise wipe: reboot failed", "err", err)
	}
}

func scheduleDeleteFileOnNextBoot(path string) error {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return fmt.Errorf("refusing empty delete path")
	}
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	moveFileEx := kernel32.NewProc("MoveFileExW")
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r1, _, e := moveFileEx.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(moveFileDelayUntilReboot),
	)
	if r1 == 0 {
		if e != nil && e.Error() != "The operation completed successfully." {
			return e
		}
		return fmt.Errorf("MoveFileExW failed for %s", path)
	}
	return nil
}

func rebootWindowsForWipe() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("reboot: open process token: %w", err)
	}
	defer token.Close()

	privName, err := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return err
	}
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, privName, &luid); err != nil {
		return fmt.Errorf("reboot: lookup privilege: %w", err)
	}
	var tp windows.Tokenprivileges
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED
	if err := windows.AdjustTokenPrivileges(token, false, &tp, uint32(unsafe.Sizeof(tp)), nil, nil); err != nil {
		return fmt.Errorf("reboot: adjust privileges: %w", err)
	}

	flags := uint32(windows.EWX_REBOOT | windows.EWX_FORCE)
	reason := uint32(windows.SHTDN_REASON_MAJOR_OTHER | windows.SHTDN_REASON_FLAG_PLANNED)
	if err := windows.ExitWindowsEx(flags, reason); err != nil {
		return fmt.Errorf("reboot: ExitWindowsEx: %w", err)
	}
	return nil
}
