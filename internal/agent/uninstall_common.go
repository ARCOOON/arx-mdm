package agent

import "errors"

// UninstallOptions configures the Windows agent uninstall (-uninstall).
type UninstallOptions struct {
	// CertDir is the directory holding client.key, client.crt, root_ca.pem.
	// Empty means <directory-of-os.Executable>/certs.
	CertDir string
}

// ErrUninstallPlatform is returned when RunUninstall is invoked on a non-Windows build.
var ErrUninstallPlatform = errors.New("agent: native uninstall is only implemented on Windows")
