package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/mdm/compliance"
	"github.com/ARCOOON/arx-mdm/internal/mdm/policy"

	"github.com/google/uuid"
)

func enforceDeclarativeProfiles(logger *slog.Logger, profiles []telemetryProfileEnvelope) {
	noteMDMPolicyEnforcement(nil, false)
	if len(profiles) == 0 {
		return
	}
	inputs := make([]policy.AssignedPayload, 0, len(profiles))
	for _, p := range profiles {
		pid, err := uuid.Parse(strings.TrimSpace(p.ID))
		if err != nil {
			noteMDMPolicyEnforcement(fmt.Errorf("configuration profile id: %w", err), true)
			if logger != nil {
				logger.Warn("mdm profile id invalid", "raw_id", p.ID, "err", err)
			}
			return
		}
		if pid == uuid.Nil {
			noteMDMPolicyEnforcement(fmt.Errorf("configuration profile id is empty"), true)
			if logger != nil {
				logger.Warn("mdm profile id empty", "raw_id", p.ID)
			}
			return
		}
		raw := p.Payload
		if len(raw) == 0 {
			raw = json.RawMessage([]byte("{}"))
		}
		inputs = append(inputs, policy.AssignedPayload{
			Source:  policy.ProfileSource{ID: pid, Name: p.Name},
			Payload: raw,
		})
	}
	mr, err := policy.MergeAssignedPayloads(inputs)
	if err != nil {
		noteMDMPolicyEnforcement(err, true)
		if logger != nil {
			logger.Warn("mdm profile merge failed", "err", err)
		}
		return
	}
	raw, err := json.Marshal(mr.EffectivePayload)
	if err != nil {
		noteMDMPolicyEnforcement(err, true)
		return
	}
	critical := compliance.PayloadCarriesCriticalSecurityControls(mr.EffectivePayload)
	enforceEffectiveMergedPayload(logger, runtime.GOOS, raw, critical)
}

func enforceEffectiveMergedPayload(logger *slog.Logger, osFamily string, payload json.RawMessage, criticalPayload bool) {
	err := platformEnforceMergedPayload(logger, osFamily, payload)
	appliesCritical := criticalPayload && err != nil
	noteMDMPolicyEnforcement(err, appliesCritical)
	if err != nil && logger != nil {
		logger.Warn("mdm merged policy enforcement failed", "err", err)
	}
}
