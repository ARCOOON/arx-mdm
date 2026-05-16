package policy

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ProfileSource identifies one configuration profile contributing to the merge.
type ProfileSource struct {
	ID   uuid.UUID
	Name string
}

// ConflictProfile summarizes one participating profile in a conflicting merge decision.
type ConflictProfile struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SettingConflict describes divergent inputs before restrictive merge at one JSON path.
type SettingConflict struct {
	Path                  string            `json:"path"`
	EffectiveValue        any               `json:"effective_value"`
	ConflictingProfiles   []ConflictProfile `json:"conflicting_profiles"`
	ContributedNormalized []any             `json:"contributed_normalized,omitempty"`
}

// EffectiveSetting is one flattened leaf for operator review (dashboard UI).
type EffectiveSetting struct {
	Path           string            `json:"path"`
	Value          any               `json:"value"`
	Conflict       bool              `json:"conflict"`
	SourceProfiles []ConflictProfile `json:"source_profiles,omitempty"`
}

// MergeResult holds merged JSON plus diagnostics for APIs and reconciliation.
type MergeResult struct {
	EffectivePayload map[string]any     `json:"effective_payload"`
	FlatSettings     []EffectiveSetting `json:"settings"`
	Conflicts        []SettingConflict  `json:"conflicts,omitempty"`
	ConflictPaths    map[string]struct{}
	ConflictProfiles map[uuid.UUID]struct{}
}

// AssignedPayload is minimal profile metadata plus declarative payload bytes.
type AssignedPayload struct {
	Source  ProfileSource
	Payload json.RawMessage
}
