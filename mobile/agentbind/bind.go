// Package agentbind is gomobile-bindable agent telemetry for the Android shell app.
package agentbind

import (
	"encoding/json"

	"github.com/ARCOOON/arx-mdm/internal/agent/telemetry"
)

// Version returns the bind library version string.
func Version() string {
	return "0.1.0"
}

// CollectTelemetryJSON returns a JSON object with host metrics from gopsutil.
func CollectTelemetryJSON() (string, error) {
	snap, err := telemetry.Collect()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
