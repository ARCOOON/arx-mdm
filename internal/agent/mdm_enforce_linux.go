//go:build linux && !android

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type linuxProfileUnion struct {
	Sysctl        map[string]string `json:"sysctl"`
	LinuxFirewall *linuxFWBlock     `json:"linux_firewall"`
	Firewall      *linuxFWBlock     `json:"firewall"`
}

type linuxFWBlock struct {
	Sysctl map[string]string `json:"sysctl"`
}

func handleLinuxDeclarative(logger *slog.Logger, declaredType string, raw json.RawMessage) error {
	obj := linuxProfileUnion{}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}

	if declaredType == "firewall" {
		fw := obj.LinuxFirewall
		if fw == nil {
			fw = obj.Firewall
		}
		if fw != nil && len(fw.Sysctl) > 0 {
			obj.Sysctl = mergeStringMaps(obj.Sysctl, fw.Sysctl)
		}
	}

	var errs []error

	if len(obj.Sysctl) > 0 {
		if err := applyLinuxSysctl(obj.Sysctl); err != nil {
			errs = append(errs, err)
		}
	}

	stateDir := "/var/lib/arx-mdm"
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		errs = append(errs, fmt.Errorf("mdm linux cannot ensure state dir: %w", err))
	}

	if declaredType != "" {
		path := filepath.Join(stateDir, "last-declared-profile-type-"+sanitizeMDMFilename(declaredType)+".txt")
		if werr := os.WriteFile(path, []byte(declaredType), 0o644); werr != nil {
			errs = append(errs, fmt.Errorf("mdm linux persisted profile marker failed: %w", werr))
		}
	}

	if logger != nil {
		logger.Debug("mdm linux declarative profile applied", "type", declaredType)
	}
	return errors.Join(errs...)
}

func mergeStringMaps(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[strings.TrimSpace(k)] = v
	}
	for k, v := range extra {
		out[strings.TrimSpace(k)] = v
	}
	return out
}

func sanitizeMDMFilename(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			continue
		}
		if b.Len() >= 96 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func applyLinuxSysctl(values map[string]string) error {
	for k, v := range values {
		keyPath := sysctlKeyToProcPath(strings.TrimSpace(k))
		raw := strings.TrimSpace(v)
		if err := sysctlWriteValidated(keyPath, raw); err != nil {
			return fmt.Errorf("%s: %w", keyPath, err)
		}
	}
	return nil
}

func sysctlKeyToProcPath(key string) string {
	key = strings.ReplaceAll(strings.TrimSpace(key), ".", string(os.PathSeparator))
	return filepath.Join("/proc/sys", key)
}

func sysctlWriteValidated(fullPath string, value string) error {
	clean := filepath.Clean(fullPath)
	root := filepath.Clean("/proc/sys")
	if root == "." {
		return fmt.Errorf("invalid sysctl root")
	}
	if !strings.HasPrefix(clean, root+string(os.PathSeparator)) || clean == root {
		return fmt.Errorf("sysctl path outside /proc/sys: %s", fullPath)
	}

	return os.WriteFile(clean, []byte(value+"\n"), 0o644)
}
