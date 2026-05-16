//go:build windows

package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type windowsDeclarativeEnvelope struct {
	Windows *windowsSubtree `json:"windows"`
	WiFiXML string          `json:"wifi_profile_xml"`

	Registry []registryHiveBlock `json:"registry"`

	WindowsFirewall *windowsFirewallBlock `json:"windows_firewall"`
	Firewall        *windowsFirewallBlock `json:"firewall"`
	WiFi            *windowsWiFiBlock     `json:"wifi"`
}

type windowsSubtree struct {
	Registry        []registryHiveBlock   `json:"registry"`
	WiFi            *windowsWiFiBlock     `json:"wifi"`
	WiFiProfileXML  string                `json:"wifi_profile_xml"`
	WindowsFirewall *windowsFirewallBlock `json:"windows_firewall"`
	Firewall        *windowsFirewallBlock `json:"firewall"`
}

type registryHiveBlock struct {
	Hive   string         `json:"hive"`
	Path   string         `json:"path"`
	Values []registryCell `json:"values"`
}

type registryCell struct {
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Data  string  `json:"data"`
	DWord *uint32 `json:"uint32,omitempty"`
	QWord *uint64 `json:"uint64,omitempty"`
}

type windowsFirewallBlock struct {
	DomainProfileEnableFirewall   bool `json:"domain_profile_enable_firewall"`
	StandardProfileEnableFirewall bool `json:"standard_profile_enable_firewall"`
}

type windowsWiFiBlock struct {
	ProvisioningXML string `json:"profile_xml"`
}

func handleWindowsDeclarative(logger *slog.Logger, declared string, raw json.RawMessage) error {
	env := windowsDeclarativeEnvelope{}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}

	var regBlocks []registryHiveBlock
	xmlPayload := ""
	var fwBlock *windowsFirewallBlock

	root := env.Windows
	if root != nil {
		regBlocks = append(regBlocks, root.Registry...)
		if root.WiFi != nil {
			xmlPayload = strings.TrimSpace(root.WiFi.ProvisioningXML)
		}
		if strings.TrimSpace(root.WiFiProfileXML) != "" {
			xmlPayload = strings.TrimSpace(root.WiFiProfileXML)
		}
		if root.WindowsFirewall != nil {
			fwBlock = root.WindowsFirewall
		}
		if root.Firewall != nil {
			fwBlock = mergeWindowsFirewall(fwBlock, root.Firewall)
		}
	}
	regBlocks = append(regBlocks, env.Registry...)
	if env.WiFi != nil && strings.TrimSpace(env.WiFi.ProvisioningXML) != "" {
		xmlPayload = strings.TrimSpace(env.WiFi.ProvisioningXML)
	}
	if strings.TrimSpace(env.WiFiXML) != "" {
		xmlPayload = strings.TrimSpace(env.WiFiXML)
	}
	if env.WindowsFirewall != nil {
		fwBlock = mergeWindowsFirewall(fwBlock, env.WindowsFirewall)
	}
	if env.Firewall != nil {
		fwBlock = mergeWindowsFirewall(fwBlock, env.Firewall)
	}

	var errs []error

	if declared == "wifi" || xmlPayload != "" {
		if err := persistWindowsWiFiProfile(xmlPayload); err != nil {
			errs = append(errs, err)
		}
	}

	if fwBlock != nil {
		if err := applyWindowsFirewallPolicy(fwBlock); err != nil {
			errs = append(errs, err)
		}
	}

	for _, blk := range regBlocks {
		if err := applyRegistryHiveBlock(logger, blk); err != nil {
			errs = append(errs, err)
		}
	}

	if logger != nil {
		logger.Debug("mdm windows declarative handled", "declared_type", declared)
	}
	return errors.Join(errs...)
}

func mergeWindowsFirewall(primary, overlay *windowsFirewallBlock) *windowsFirewallBlock {
	if primary == nil {
		return overlay
	}
	if overlay == nil {
		return primary
	}
	next := *primary
	if overlay.DomainProfileEnableFirewall {
		next.DomainProfileEnableFirewall = true
	}
	if overlay.StandardProfileEnableFirewall {
		next.StandardProfileEnableFirewall = true
	}
	return &next
}

func resolveHive(raw string) registry.Key {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "", "HKLM", "HKEY_LOCAL_MACHINE":
		return registry.LOCAL_MACHINE
	case "HKCU", "HKEY_CURRENT_USER":
		return registry.CURRENT_USER
	default:
		return registry.LOCAL_MACHINE
	}
}

func applyRegistryHiveBlock(logger *slog.Logger, blk registryHiveBlock) error {
	sub := strings.TrimSpace(blk.Path)
	if sub == "" {
		return nil
	}

	rootKey := resolveHive(blk.Hive)
	key, _, err := registry.CreateKey(rootKey, sub, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()

	for _, cell := range blk.Values {
		name := strings.TrimSpace(cell.Name)
		kind := strings.ToUpper(strings.TrimSpace(cell.Kind))
		switch kind {
		case "", "SZ", "REG_SZ":
			if err := key.SetStringValue(name, strings.TrimSpace(cell.Data)); err != nil {
				return err
			}
		case "DWORD", "REG_DWORD":
			val := cell.DWord
			if val == nil {
				continue
			}
			if err := key.SetDWordValue(name, *val); err != nil {
				return err
			}
		case "QWORD", "REG_QWORD":
			val := cell.QWord
			if val == nil {
				continue
			}
			if err := key.SetQWordValue(name, *val); err != nil {
				return err
			}
		case "EXPAND_SZ", "REG_EXPAND_SZ":
			if err := key.SetExpandStringValue(name, strings.TrimSpace(cell.Data)); err != nil {
				return err
			}
		default:
			if logger != nil {
				logger.Warn("mdm registry kind skipped", "kind", cell.Kind)
			}
		}
	}
	return nil
}

func applyWindowsFirewallPolicy(block *windowsFirewallBlock) error {
	if block == nil {
		return nil
	}
	domainEnable := block.DomainProfileEnableFirewall
	standardEnable := block.StandardProfileEnableFirewall
	if !domainEnable && !standardEnable {
		return nil
	}
	if _, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Microsoft\WindowsFirewall`, registry.ALL_ACCESS); err != nil {
		return err
	}
	if domainEnable {
		k, _, err := registry.CreateKey(registry.LOCAL_MACHINE,
			`SOFTWARE\Policies\Microsoft\WindowsFirewall\DomainProfile`, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		err = k.SetDWordValue("EnableFirewall", 1)
		kCloseErr := k.Close()
		if err != nil {
			return err
		}
		if kCloseErr != nil {
			return kCloseErr
		}
	}
	if standardEnable {
		k, _, err := registry.CreateKey(registry.LOCAL_MACHINE,
			`SOFTWARE\Policies\Microsoft\WindowsFirewall\StandardProfile`, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		err = k.SetDWordValue("EnableFirewall", 1)
		kCloseErr := k.Close()
		if err != nil {
			return err
		}
		if kCloseErr != nil {
			return kCloseErr
		}
	}
	return nil
}

func persistWindowsWiFiProfile(xml string) error {
	xml = strings.TrimSpace(xml)
	if xml == "" {
		return nil
	}
	base := os.Getenv("ProgramData")
	if base == "" {
		base = filepath.Join(os.Getenv("SystemDrive")+"\\", "ProgramData")
	}
	dir := filepath.Join(base, "arx-mdm", "wifi-managed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	target := filepath.Join(dir, "managed-profile.xml")
	return os.WriteFile(target, []byte(xml), 0o644)
}
