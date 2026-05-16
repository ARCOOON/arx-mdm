package models

import (
	"time"

	"github.com/google/uuid"
)

// AlertRule defines a persisted metric/offline threshold evaluated by the alerting engine.
type AlertRule struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	TargetType      string     `json:"target_type"`
	Metric          string     `json:"metric"`
	Operator        string     `json:"operator"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int64   `json:"duration_seconds"`
	Severity        string  `json:"severity"`
	IsEnabled       bool       `json:"is_enabled"`
	TargetDeviceID  *uuid.UUID `json:"target_device_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ActiveAlert is an open or historical fired alert row.
type ActiveAlert struct {
	ID              uuid.UUID  `json:"id"`
	AlertRuleID     *uuid.UUID `json:"alert_rule_id,omitempty"`
	Fingerprint     string     `json:"fingerprint"`
	AlertKind       string     `json:"alert_kind"`
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	Severity        string     `json:"severity"`
	Title           string     `json:"title"`
	Message         string     `json:"message"`
	Details         []byte     `json:"-"`
	TriggeredAt     time.Time  `json:"triggered_at"`
	LastNotifiedAt  *time.Time `json:"last_notified_at,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}

// NotificationChannel is an SMTP, Slack-style webhook, or generic webhook destination.
type NotificationChannel struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	ChannelType   string    `json:"channel_type"`
	ConfigJSON    []byte    `json:"-"`
	SigningSecret string    `json:"-"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
