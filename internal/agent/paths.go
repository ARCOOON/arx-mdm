package agent

import "path/filepath"

const (
	defaultCertDir = "certs"
	maxBodyBytes   = 4 << 20
)

// DefaultCertDir returns the default relative directory for enrollment material.
func DefaultCertDir() string {
	return defaultCertDir
}

// ClientMaterialPaths returns paths to client.key, client.crt, and root_ca.pem under certDir.
func ClientMaterialPaths(certDir string) (keyPath, certPath, rootPath string) {
	d := filepath.Clean(certDir)
	return filepath.Join(d, "client.key"), filepath.Join(d, "client.crt"), filepath.Join(d, "root_ca.pem")
}
