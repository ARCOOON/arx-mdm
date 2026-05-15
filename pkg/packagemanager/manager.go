// Package packagemanager provides native inventory of installed software and a
// narrowly scoped execution path for invoking system package managers (no shell wrappers).
package packagemanager

import "context"

// InstalledApp is a single inventory row reported to the MDM server (telemetry).
type InstalledApp struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"` // registry, dpkg, …
	ID      string `json:"id,omitempty"`
}

// MaxReportedInventory caps telemetry payload size for installed software.
const MaxReportedInventory = 500

// ListInstalled returns native inventory (registry on Windows, dpkg status on Debian/Ubuntu).
func ListInstalled() ([]InstalledApp, error) {
	return listInstalled()
}

// Install invokes the appropriate package manager binary with fixed arguments.
func Install(ctx context.Context, typ, name, version, installCmd string) error {
	return runInstall(ctx, typ, name, version, installCmd)
}

// Uninstall removes software using the same allowlisted binaries as Install.
func Uninstall(ctx context.Context, typ, name, version, installCmd string) error {
	return runUninstall(ctx, typ, name, version, installCmd)
}
