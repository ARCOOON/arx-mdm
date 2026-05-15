//go:build windows

package agent

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ARCOOON/arx-mdm/pkg/system"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// arxRegistryHKLMSubkeys are HKLM subtrees created by the ARX agent installer.
// Uninstall removes each tree recursively (including WOW6432Node for 32-bit installs).
var arxRegistryHKLMSubkeys = []string{
	`SOFTWARE\ARX\MDM\Agent`,
	`SOFTWARE\WOW6432Node\ARX\MDM\Agent`,
}

// RunUninstall stops and deletes the Windows service, removes certificate material,
// and deletes ARX-specific registry keys. Requires elevated privileges.
func RunUninstall(logger *slog.Logger, opts UninstallOptions) error {
	if logger == nil {
		return errors.New("agent: logger is required")
	}

	if err := stopAndDeleteWindowsService(logger, system.AgentServiceName); err != nil {
		return err
	}

	certAbs, err := resolveUninstallCertDir(opts.CertDir)
	if err != nil {
		return err
	}
	if err := safeCertDirForWipe(certAbs); err != nil {
		return err
	}
	if _, err := os.Stat(certAbs); err == nil {
		logger.Info("uninstall removing certificate directory", "path", certAbs)
		if err := os.RemoveAll(certAbs); err != nil {
			return fmt.Errorf("remove cert directory: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat cert directory: %w", err)
	} else {
		logger.Info("uninstall certificate directory absent, skipping", "path", certAbs)
	}

	for _, sub := range arxRegistryHKLMSubkeys {
		logger.Info("uninstall removing registry tree", "root", "HKLM", "subkey", sub)
		if err := deleteRegistryTreeHKLM(logger, sub); err != nil {
			return err
		}
	}

	logger.Info("uninstall completed successfully")
	return nil
}

func resolveUninstallCertDir(certDirOpt string) (string, error) {
	d := strings.TrimSpace(certDirOpt)
	if d != "" {
		return filepath.Abs(filepath.Clean(d))
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), defaultCertDir), nil
}

func safeCertDirForWipe(abs string) error {
	c := filepath.Clean(abs)
	if c == "." || c == "" {
		return fmt.Errorf("refusing empty cert directory path")
	}
	if vol := filepath.VolumeName(c); vol != "" {
		rest := strings.TrimPrefix(strings.TrimPrefix(c, vol), `\`)
		rest = strings.TrimPrefix(rest, `/`)
		if rest == "" {
			return fmt.Errorf("refusing to wipe drive root %s", vol)
		}
	}
	return nil
}

func stopAndDeleteWindowsService(logger *slog.Logger, name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service control manager connect: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			logger.Info("uninstall windows service not installed, skipping", "service", name)
			return nil
		}
		return fmt.Errorf("open service %q: %w", name, err)
	}
	defer s.Close()

	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service %q: %w", name, err)
	}

	if st.State != svc.Stopped {
		logger.Info("uninstall stopping windows service", "service", name, "state", strconv.FormatUint(uint64(st.State), 10))
		if _, err := s.Control(svc.Stop); err != nil {
			// Service may already be stopping or not accept stop in edge cases.
			logger.Info("uninstall service stop control returned error (continuing)", "service", name, "err", err.Error())
		}
		deadline := time.Now().Add(60 * time.Second)
		for {
			st, err = s.Query()
			if err != nil {
				return fmt.Errorf("query service after stop: %w", err)
			}
			if st.State == svc.Stopped {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for service %q to stop (state=%d)", name, st.State)
			}
			time.Sleep(300 * time.Millisecond)
		}
	} else {
		logger.Info("uninstall windows service already stopped", "service", name)
	}

	logger.Info("uninstall deleting windows service registration", "service", name)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service %q: %w", name, err)
	}
	if err := eventlog.Remove(name); err != nil {
		logger.Info("uninstall event log source removal returned error (continuing)", "err", err.Error())
	}
	return nil
}

func deleteRegistryTreeHKLM(logger *slog.Logger, subPath string) error {
	subPath = strings.Trim(subPath, `\`)
	if subPath == "" {
		return fmt.Errorf("empty registry subpath")
	}
	if err := deleteRegistryKeyRecursive(registry.LOCAL_MACHINE, subPath); err != nil {
		if isRegistryNotExist(err) {
			logger.Info("uninstall registry tree absent, skipping", "subkey", subPath)
			return nil
		}
		return fmt.Errorf("delete registry HKLM\\%s: %w", subPath, err)
	}
	return nil
}

func isRegistryNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_PATH_NOT_FOUND:
			return true
		}
	}
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND)
}

func deleteRegistryKeyRecursive(root registry.Key, subPath string) error {
	k, err := registry.OpenKey(root, subPath, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if isRegistryNotExist(err) {
			return nil
		}
		return err
	}

	subNames, err := k.ReadSubKeyNames(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		k.Close()
		return err
	}
	if err := k.Close(); err != nil {
		return err
	}

	for _, n := range subNames {
		child := subPath + `\` + n
		if err := deleteRegistryKeyRecursive(root, child); err != nil {
			return err
		}
	}

	k2, err := registry.OpenKey(root, subPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if isRegistryNotExist(err) {
			return nil
		}
		return err
	}
	valNames, err := k2.ReadValueNames(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		k2.Close()
		return err
	}
	for _, vn := range valNames {
		_ = k2.DeleteValue(vn)
	}
	if err := k2.Close(); err != nil {
		return err
	}

	parent, leaf := splitRegistrySubPath(subPath)
	if parent == "" {
		return fmt.Errorf("refusing to delete top-level registry leaf %q", leaf)
	}

	pk, err := registry.OpenKey(root, parent, registry.SET_VALUE|registry.ENUMERATE_SUB_KEYS|registry.WRITE)
	if err != nil {
		return err
	}
	defer pk.Close()

	if err := registry.DeleteKey(pk, leaf); err != nil {
		if isRegistryNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func splitRegistrySubPath(subPath string) (parent, leaf string) {
	subPath = strings.Trim(subPath, `\`)
	i := strings.LastIndex(subPath, `\`)
	if i < 0 {
		return "", subPath
	}
	return subPath[:i], subPath[i+1:]
}
