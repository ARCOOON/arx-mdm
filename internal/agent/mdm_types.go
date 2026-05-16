package agent

import "encoding/json"

// telemetryProfileEnvelope mirrors both agent pull and telemetry acknowledgement payloads.
type telemetryProfileEnvelope struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	ProfileType string          `json:"profile_type"`
	Platform    string          `json:"platform"`
	Payload     json.RawMessage `json:"payload"`
}
