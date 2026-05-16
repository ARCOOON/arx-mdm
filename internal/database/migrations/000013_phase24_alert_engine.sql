-- Phase 24: alerting engine, active alerts, and notification_channels.

CREATE TABLE IF NOT EXISTS alert_rules (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL,
    target_type       TEXT NOT NULL,
    metric            TEXT NOT NULL,
    operator          TEXT NOT NULL,
    threshold         DOUBLE PRECISION NOT NULL,
    duration          INTERVAL NOT NULL,
    severity          TEXT NOT NULL,
    is_enabled        BOOLEAN NOT NULL DEFAULT true,
    target_device_id  UUID REFERENCES assets (id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alert_rules_target_chk CHECK (target_type IN ('device', 'system')),
    CONSTRAINT alert_rules_operator_chk CHECK (operator IN ('>', '<', '>=', '<=', '==')),
    CONSTRAINT alert_rules_severity_chk CHECK (severity IN ('info', 'warning', 'critical')),
    CONSTRAINT alert_rules_metric_device_chk CHECK (
        metric IN ('cpu_usage', 'offline_status', 'ram_usage_percent', 'disk_usage_percent')
    )
);

CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules (is_enabled);

CREATE TABLE IF NOT EXISTS active_alerts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_rule_id     UUID REFERENCES alert_rules (id) ON DELETE SET NULL,
    fingerprint       TEXT NOT NULL UNIQUE,
    alert_kind        TEXT NOT NULL DEFAULT 'rule_metric',
    device_id         UUID REFERENCES assets (id) ON DELETE SET NULL,
    severity          TEXT NOT NULL,
    title             TEXT NOT NULL,
    message           TEXT NOT NULL,
    details           JSONB NOT NULL DEFAULT '{}'::jsonb,
    triggered_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_notified_at  TIMESTAMPTZ NULL,
    resolved_at       TIMESTAMPTZ NULL,
    CONSTRAINT active_alerts_severity_chk CHECK (severity IN ('info', 'warning', 'critical')),
    CONSTRAINT active_alerts_kind_chk CHECK (
        alert_kind IN ('builtin_offline', 'rule_offline', 'rule_metric', 'event_fanout')
    )
);

CREATE INDEX IF NOT EXISTS idx_active_alerts_unresolved ON active_alerts (triggered_at DESC)
    WHERE resolved_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_active_alerts_device ON active_alerts (device_id)
    WHERE resolved_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_channels (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL DEFAULT '',
    channel_type     TEXT NOT NULL,
    config_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    signing_secret   TEXT NOT NULL DEFAULT '',
    is_active        BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT notification_channels_type_chk CHECK (channel_type IN ('smtp', 'webhook', 'slack'))
);

CREATE INDEX IF NOT EXISTS idx_notification_channels_active ON notification_channels (is_active);

-- One-time migration from Phase 15 alert_settings (same IDs for stable URLs).
INSERT INTO notification_channels (
    id, name, channel_type, config_json, signing_secret, is_active, created_at, updated_at
)
SELECT a.id,
       CASE WHEN a.type = 'smtp' THEN 'SMTP channel' ELSE 'Webhook channel' END,
       a.type,
       a.config_json,
       '',
       a.is_active,
       a.created_at,
       a.updated_at
FROM alert_settings a
ON CONFLICT (id) DO NOTHING;

DROP TABLE IF EXISTS alert_stale_ack;
