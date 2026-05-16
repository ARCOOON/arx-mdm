package agentbind

import (
	"github.com/ARCOOON/arx-mdm/internal/agent/c2"
)

func init() {
	c2.SetAndroidQuarantineHook(dispatchAndroidQuarantineLocked)
}

func dispatchAndroidQuarantineLocked(enabled bool) {
	androidSecMu.Lock()
	s := androidSec
	androidSecMu.Unlock()
	if s != nil {
		s.ApplyQuarantine(enabled)
	}
}
