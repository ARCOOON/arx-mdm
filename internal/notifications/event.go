package notifications

// Alert payload types emitted by alerting and integrations.
const (
	TypeSMTP           = "smtp"
	TypeWebhook        = "webhook"
	TypeSlackIncoming  = "slack"
	EventDeviceOffline = "device_offline"
	EventRuleMetric    = "alert_rule_metric"
	EventRuleOffline   = "alert_rule_offline"

	EventAssetStaleHeartbeat = "asset_stale_heartbeat" // legacy webhook consumers
	EventAndroidRemoteWipe   = "android_remote_wipe"
	EventTicketINCCreated    = "ticket_inc_created"
)

// AlertEvent is a normalized outbound notification emitted by ARX MDM.
type AlertEvent struct {
	Type     string         `json:"type"`
	Severity string         `json:"severity,omitempty"`
	Title    string         `json:"title"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}
