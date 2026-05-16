package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ConfigurationProfile is a platform-scoped declarative endpoint policy envelope.
type ConfigurationProfile struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Platform  string          `json:"platform"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// ProfileAssignment binds a profile to a single managed device or a principal group cohort.
type ProfileAssignment struct {
	ID               uuid.UUID  `json:"id"`
	ProfileID        uuid.UUID  `json:"profile_id"`
	TargetKind       string     `json:"target_kind"`
	DeviceID         *uuid.UUID `json:"device_id,omitempty"`
	PrincipalGroupID *uuid.UUID `json:"principal_group_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	AssignmentState  string     `json:"assignment_state"`
}

// PrincipalGroup is a cohort label for assigning configuration profiles wholesale.
type PrincipalGroup struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// AppManagedConfiguration exposes App Config key-value payloads for deployments.
type AppManagedConfiguration struct {
	ID                 uuid.UUID       `json:"id"`
	CatalogAppID       uuid.UUID       `json:"catalog_app_id"`
	ManagedPackageName string          `json:"managed_package_name"`
	ManagedAppLabel    string          `json:"managed_app_label"`
	ConfigKV           json.RawMessage `json:"config_kv"`
	CreatedAt          time.Time       `json:"created_at"`
}
