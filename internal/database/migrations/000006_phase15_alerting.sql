-- Phase 15: alerting & outbound notifications (self-hosted SMTP / webhooks).

CREATE TABLE IF NOT EXISTS alert_settings (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type         TEXT NOT NULL,
    config_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alert_settings_type_values CHECK (type IN ('smtp', 'webhook'))
);

CREATE INDEX IF NOT EXISTS idx_alert_settings_type_active ON alert_settings (type, is_active);

CREATE TABLE IF NOT EXISTS alert_stale_ack (
    asset_id   UUID PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    alerted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
