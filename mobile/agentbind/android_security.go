//go:build android

package agentbind

import "sync"

// AndroidSecurity surfaces device-owner lock and wipe hooks implemented in Kotlin.
type AndroidSecurity interface {
	LockDevice()
	WipeEnterprise()
}

var androidSecMu sync.Mutex
var androidSec AndroidSecurity

// RegisterAndroidSecurity wires Kotlin implementations for remote lock and enterprise wipe.
func RegisterAndroidSecurity(s AndroidSecurity) {
	androidSecMu.Lock()
	androidSec = s
	androidSecMu.Unlock()
}

// DispatchAndroidLock invokes the registered Kotlin lock handler.
func DispatchAndroidLock() {
	androidSecMu.Lock()
	s := androidSec
	androidSecMu.Unlock()
	if s != nil {
		s.LockDevice()
	}
}

// DispatchAndroidWipe invokes the registered Kotlin wipe handler.
func DispatchAndroidWipe() {
	androidSecMu.Lock()
	s := androidSec
	androidSecMu.Unlock()
	if s != nil {
		s.WipeEnterprise()
	}
}
