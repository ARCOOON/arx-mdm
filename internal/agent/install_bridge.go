package agent

import (
	"sync/atomic"
)

// InstallBridge supplies server identity material for ancillary HTTPS downloads initiated by catalog installs.
type InstallBridge struct {
	ServerURL string
	CertDir   string
}

var activeInstallBridge atomic.Pointer[InstallBridge]

// SetInstallBridge records the runtime server URL/certificate directory for nested download tasks.
func SetInstallBridge(b *InstallBridge) {
	activeInstallBridge.Store(b)
}

// ActiveInstallBridge returns the active InstallBridge configured for the connected WebSocket session.
func ActiveInstallBridge() *InstallBridge {
	return activeInstallBridge.Load()
}
