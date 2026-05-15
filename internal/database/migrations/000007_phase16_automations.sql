-- Phase 16: scheduled automations (cron → C2 dispatch).

CREATE TABLE IF NOT EXISTS automations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    cron_schedule   TEXT NOT NULL,
    action_type     TEXT NOT NULL,
    target_os       TEXT,
    target_asset_id UUID REFERENCES assets (id) ON DELETE CASCADE,
    payload_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT automations_action_type_values CHECK (
        action_type IN ('shutdown', 'deploy_package')
    ),
    CONSTRAINT automations_target_os_values CHECK (
        target_os IS NULL OR target_os IN ('unknown', 'windows', 'linux', 'darwin', 'android', 'ios')
    ),
    CONSTRAINT automations_target_scope CHECK (
        target_asset_id IS NOT NULL
        OR (target_os IS NOT NULL AND length(trim(target_os)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS idx_automations_active ON automations (is_active);
CREATE INDEX IF NOT EXISTS idx_automations_target_asset ON automations (target_asset_id);
