package agent

import (
	"sync"

	"github.com/ARCOOON/arx-mdm/internal/api"
)

var (
	mdmPolicyEnfMu     sync.Mutex
	mdmPolicyEnfLatest *api.MDMPolicyEnforcementWire
)

func noteMDMPolicyEnforcement(err error, criticalFailed bool) {
	mdmPolicyEnfMu.Lock()
	defer mdmPolicyEnfMu.Unlock()
	if err != nil {
		mdmPolicyEnfLatest = &api.MDMPolicyEnforcementWire{
			State:          "error",
			Detail:         err.Error(),
			CriticalFailed: criticalFailed,
		}
		return
	}
	mdmPolicyEnfLatest = &api.MDMPolicyEnforcementWire{State: "ok"}
}

func telemetryMDMPolicyEnforcementWire() *api.MDMPolicyEnforcementWire {
	mdmPolicyEnfMu.Lock()
	defer mdmPolicyEnfMu.Unlock()
	if mdmPolicyEnfLatest == nil {
		return nil
	}
	cp := *mdmPolicyEnfLatest
	return &cp
}
