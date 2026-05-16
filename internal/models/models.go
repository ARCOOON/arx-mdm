package models

import (
	"time"

	"github.com/google/uuid"
)

// Asset represents a managed endpoint or infrastructure item identified by an ARX human_id.
type Asset struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	HumanID     string     `json:"human_id" db:"human_id"`
	DisplayName string     `json:"display_name,omitempty" db:"display_name"`
	Hostname    *string    `json:"hostname,omitempty" db:"hostname"`
	CertSerial  *string    `json:"cert_serial,omitempty" db:"cert_serial"`
	OsType      string     `json:"os_type,omitempty" db:"os_type"`
	LastSeen    *time.Time `json:"last_seen,omitempty" db:"last_seen"`
	Metadata    []byte     `json:"metadata,omitempty" db:"metadata"` // JSONB as raw JSON bytes for pgx
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// Ticket is a work item using the required ticket reference prefixes (INC-, REQ-, CHG-, PRJ-).
type Ticket struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TicketRef   string     `json:"ticket_ref" db:"ticket_ref"`
	Title       string     `json:"title" db:"title"`
	Description string     `json:"description,omitempty" db:"description"`
	Status      string     `json:"status" db:"status"`
	Priority    string     `json:"priority" db:"priority"`
	DeviceID    *uuid.UUID `json:"device_id,omitempty" db:"device_id"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	AssignedTo  *uuid.UUID `json:"assigned_to,omitempty" db:"assigned_to"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// Resolution records how a ticket was closed or superseded.
type Resolution struct {
	ID         uuid.UUID `json:"id" db:"id"`
	TicketID   uuid.UUID `json:"ticket_id" db:"ticket_id"`
	Summary    string    `json:"summary" db:"summary"`
	Markdown   string    `json:"markdown" db:"markdown"`
	Details    []byte    `json:"details,omitempty" db:"details"` // JSONB
	ResolvedAt time.Time `json:"resolved_at" db:"resolved_at"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// EnrollmentToken is the persisted enrollment offer; the cleartext presentation secret is never stored.
type EnrollmentToken struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TokenHash   string     `json:"-" db:"token_hash"`
	AssetID     *uuid.UUID `json:"asset_id,omitempty" db:"asset_id"`
	ExpiresAt   time.Time  `json:"expires_at" db:"expires_at"`
	IsUsed      bool       `json:"is_used" db:"is_used"`
	UsedAt      *time.Time `json:"used_at,omitempty" db:"used_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	Provisioner *string    `json:"provisioner,omitempty" db:"provisioner"`
}

// Package is a catalog entry for software that can be deployed to managed assets.
type Package struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Version     string    `json:"version" db:"version"`
	Type        string    `json:"type" db:"type"`
	InstallCmd  string    `json:"install_cmd" db:"install_cmd"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Deployment records an assignment of a catalog package to an asset and its lifecycle status.
type Deployment struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	AssetID       uuid.UUID  `json:"asset_id" db:"asset_id"`
	PackageID     uuid.UUID  `json:"package_id" db:"package_id"`
	Status        string     `json:"status" db:"status"`
	ErrorMessage  *string    `json:"error_message,omitempty" db:"error_message"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// User is a dashboard account with a bcrypt password hash (never serialized as JSON).
type User struct {
	ID           uuid.UUID `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         string    `json:"role" db:"role"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// Document is a knowledge base entry stored as Markdown.
type Document struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	Title           string     `json:"title" db:"title"`
	ContentMarkdown string     `json:"content_markdown" db:"content_markdown"`
	UploadedBy      *uuid.UUID `json:"uploaded_by,omitempty" db:"uploaded_by"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}

// AndroidPolicy is persisted DPC policy for an Android-managed asset (one row per asset).
type AndroidPolicy struct {
	AssetID              uuid.UUID `json:"asset_id" db:"asset_id"`
	CameraDisabled       bool      `json:"camera_disabled" db:"camera_disabled"`
	ScreenLockTimeoutMs  int64     `json:"screen_lock_timeout_ms" db:"screen_lock_timeout_ms"`
	WipeRequested        bool      `json:"wipe_requested" db:"wipe_requested"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}

// AuditLog is an append-only operator or system action record (REST mutation or C&C dispatch).
type AuditLog struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UserID        *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Action        string     `json:"action" db:"action"`
	ResourceType  string     `json:"resource_type,omitempty" db:"resource_type"`
	ResourceID    *uuid.UUID `json:"resource_id,omitempty" db:"resource_id"`
	TargetAssetID *uuid.UUID `json:"target_asset_id,omitempty" db:"target_asset_id"`
	DetailsJSON   []byte     `json:"details,omitempty" db:"details"` // JSONB column "details"
	IPAddress     *string    `json:"ip_address,omitempty" db:"ip_address"`
}
