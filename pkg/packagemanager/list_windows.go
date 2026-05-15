//go:build windows

package packagemanager

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func listInstalled() ([]InstalledApp, error) {
	var out []InstalledApp
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, path := range []string{
			`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
			`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
		} {
			apps, err := enumerateUninstall(root, path)
			if err != nil {
				continue
			}
			out = append(out, apps...)
		}
	}
	return trimCap(out), nil
}

func enumerateUninstall(root registry.Key, subPath string) ([]InstalledApp, error) {
	k, err := registry.OpenKey(root, subPath, registry.READ)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []InstalledApp
	for _, name := range names {
		sk, err := registry.OpenKey(root, subPath+`\`+name, registry.READ)
		if err != nil {
			continue
		}
		display, _, err := sk.GetStringValue("DisplayName")
		if err != nil || strings.TrimSpace(display) == "" {
			sk.Close()
			continue
		}
		ver, _, _ := sk.GetStringValue("DisplayVersion")
		sk.Close()
		out = append(out, InstalledApp{
			Name:    strings.TrimSpace(display),
			Version: strings.TrimSpace(ver),
			Source:  "registry",
			ID:      name,
		})
	}
	return out, nil
}

func trimCap(in []InstalledApp) []InstalledApp {
	if len(in) <= MaxReportedInventory {
		return in
	}
	return in[:MaxReportedInventory]
}
