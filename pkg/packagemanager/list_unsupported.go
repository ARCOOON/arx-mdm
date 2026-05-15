//go:build !linux && !windows

package packagemanager

func listInstalled() ([]InstalledApp, error) {
	return nil, nil
}
