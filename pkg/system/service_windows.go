//go:build windows

package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	agentServiceDisplayName = "ARX MDM Agent"
	agentServiceDescription = "ARX MDM managed device agent (telemetry and C2 WebSocket)."
	// agentRegistryHKLM is the canonical HKLM subkey for ARX agent installer metadata (mirrors uninstall).
	agentRegistryHKLM = `SOFTWARE\ARX\MDM\Agent`
)

// InWindowsService reports whether this process was started by the Windows Service Control Manager.
func InWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

// AgentExePath returns an absolute path to the current executable, suitable for SCM binary path registration.
func AgentExePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(p), nil
}

// InstallWindowsAgentService registers the ARX agent as an auto-start Windows service using the native SCM API.
// It writes minimal registry metadata under HKLM\SOFTWARE\ARX\MDM\Agent, registers an event log source, and starts the service.
func InstallWindowsAgentService(opts WindowsAgentInstallOptions) error {
	server := strings.TrimSpace(opts.ServerURL)
	if server == "" {
		return errors.New("system: ServerURL is required")
	}

	exe, err := AgentExePath()
	if err != nil {
		return fmt.Errorf("system: resolve executable: %w", err)
	}

	var certAbs string
	if strings.TrimSpace(opts.CertDir) == "" {
		certAbs = filepath.Join(filepath.Dir(exe), "certs")
	} else {
		var errPath error
		certAbs, errPath = filepath.Abs(strings.TrimSpace(opts.CertDir))
		if errPath != nil {
			return fmt.Errorf("system: resolve cert directory: %w", errPath)
		}
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("system: service control manager connect: %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(AgentServiceName); err == nil {
		s.Close()
		return fmt.Errorf("system: service %q already exists", AgentServiceName)
	}

	cmdArgs := []string{"run", "-server", server, "-certdir", certAbs}
	if opts.Interval > 0 {
		cmdArgs = append(cmdArgs, "-interval", opts.Interval.String())
	}

	s, err := m.CreateService(AgentServiceName, exe, mgr.Config{
		DisplayName: agentServiceDisplayName,
		Description: agentServiceDescription,
		StartType:   mgr.StartAutomatic,
	}, cmdArgs...)
	if err != nil {
		return fmt.Errorf("system: create service: %w", err)
	}
	defer s.Close()

	if err := eventlog.InstallAsEventCreate(AgentServiceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		_ = s.Delete()
		return fmt.Errorf("system: event log source install: %w", err)
	}

	if err := writeAgentInstallRegistry(exe, server, certAbs); err != nil {
		_ = eventlog.Remove(AgentServiceName)
		_ = s.Delete()
		return err
	}

	if err := s.Start(); err != nil {
		return fmt.Errorf("system: start service: %w", err)
	}
	return nil
}

// AgentInstallMetadata is persisted under HKLM\SOFTWARE\ARX\MDM\Agent for service installs.
type AgentInstallMetadata struct {
	InstallExe string
	ServerURL  string
	CertDir    string
}

// ReadAgentInstallMetadata reads install paths written by InstallWindowsAgentService.
func ReadAgentInstallMetadata() (AgentInstallMetadata, error) {
	var out AgentInstallMetadata
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegistryHKLM, registry.QUERY_VALUE)
	if err != nil {
		return out, fmt.Errorf("system: open registry HKLM\\%s: %w", agentRegistryHKLM, err)
	}
	defer k.Close()

	if s, _, err := k.GetStringValue("InstallExe"); err == nil {
		out.InstallExe = strings.TrimSpace(s)
	}
	if s, _, err := k.GetStringValue("ServerURL"); err == nil {
		out.ServerURL = strings.TrimSpace(s)
	}
	if s, _, err := k.GetStringValue("CertDir"); err == nil {
		out.CertDir = strings.TrimSpace(s)
	}
	if out.InstallExe == "" || out.CertDir == "" {
		return out, fmt.Errorf("system: incomplete agent install metadata in registry")
	}
	return out, nil
}

// QueryAgentServiceExePath parses SCM-registered BinaryPathName for [AgentServiceName] and returns the executable path.
func QueryAgentServiceExePath() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("system: scm connect: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(AgentServiceName)
	if err != nil {
		return "", fmt.Errorf("system: open service %q: %w", AgentServiceName, err)
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil {
		return "", fmt.Errorf("system: service config: %w", err)
	}
	raw := strings.TrimSpace(cfg.BinaryPathName)
	if raw == "" {
		return "", fmt.Errorf("system: empty service binary path")
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", fmt.Errorf("system: could not parse service binary path")
	}
	exe := strings.Trim(fields[0], `"'`)
	return filepath.Clean(exe), nil
}

func writeAgentInstallRegistry(exe, serverURL, certAbs string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, agentRegistryHKLM, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("system: create registry key HKLM\\%s: %w", agentRegistryHKLM, err)
	}
	defer k.Close()

	if err := k.SetStringValue("InstallExe", exe); err != nil {
		return fmt.Errorf("system: set InstallExe: %w", err)
	}
	if err := k.SetStringValue("ServerURL", serverURL); err != nil {
		return fmt.Errorf("system: set ServerURL: %w", err)
	}
	if err := k.SetStringValue("CertDir", certAbs); err != nil {
		return fmt.Errorf("system: set CertDir: %w", err)
	}
	return nil
}
