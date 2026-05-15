-- Baseline schema (PostgreSQL 13+). Idempotent: safe on empty or legacy databases.
-- Canonical source for new installs; later numbered files apply phased deltas.

CREATE TABLE IF NOT EXISTS assets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    human_id     TEXT NOT NULL UNIQUE,
    display_name TEXT,
    hostname     TEXT,
    cert_serial  TEXT,
    os_type      TEXT NOT NULL DEFAULT 'unknown',
    last_seen    TIMESTAMPTZ,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT assets_human_id_format CHECK (
        human_id ~ '^(arx-c-[0-9]+|arx-s-[0-9]+|arx-[a-z0-9]+-[a-z0-9]+-[0-9]+)$'
    ),
    CONSTRAINT assets_os_type_values CHECK (
        os_type IN ('unknown', 'windows', 'linux', 'darwin', 'android', 'ios')
    )
);

CREATE INDEX IF NOT EXISTS idx_assets_created_at ON assets (created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_hostname_unique ON assets (hostname) WHERE hostname IS NOT NULL AND hostname <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_cert_serial_unique ON assets (cert_serial) WHERE cert_serial IS NOT NULL AND cert_serial <> '';
CREATE INDEX IF NOT EXISTS idx_assets_last_seen ON assets (last_seen DESC);

CREATE TABLE IF NOT EXISTS tickets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_ref  TEXT NOT NULL UNIQUE,
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    priority    TEXT NOT NULL DEFAULT 'normal',
    asset_id    UUID REFERENCES assets (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tickets_ref_prefix CHECK (
        ticket_ref ~ '^(INC|REQ|CHG|PRJ)-[A-Za-z0-9_-]+$'
    ),
    CONSTRAINT tickets_priority_values CHECK (
        priority IN ('critical', 'high', 'normal', 'low')
    )
);

CREATE INDEX IF NOT EXISTS idx_tickets_asset_id ON tickets (asset_id);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets (status);

CREATE SEQUENCE IF NOT EXISTS ticket_seq_inc AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_req AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_chg AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_prj AS bigint START 1 INCREMENT 1;

CREATE TABLE IF NOT EXISTS resolutions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   UUID NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    summary     TEXT NOT NULL,
    markdown    TEXT NOT NULL DEFAULT '',
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_resolutions_ticket_id ON resolutions (ticket_id);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash        TEXT NOT NULL UNIQUE,
    asset_id          UUID REFERENCES assets (id) ON DELETE SET NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    is_used           BOOLEAN NOT NULL DEFAULT false,
    used_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    provisioner       TEXT
);

CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_expires_at ON enrollment_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_asset_id ON enrollment_tokens (asset_id);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_unused ON enrollment_tokens (token_hash) WHERE is_used = false;

CREATE TABLE IF NOT EXISTS packages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    version      TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL,
    install_cmd  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT packages_type_values CHECK (
        type IN ('winget', 'apt', 'dnf', 'choco', 'custom')
    )
);

CREATE INDEX IF NOT EXISTS idx_packages_type ON packages (type);
CREATE INDEX IF NOT EXISTS idx_packages_name ON packages (name);

CREATE TABLE IF NOT EXISTS deployments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id       UUID NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    package_id     UUID NOT NULL REFERENCES packages (id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'pending',
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT deployments_status_values CHECK (
        status IN ('pending', 'dispatched', 'in_progress', 'succeeded', 'failed')
    )
);

CREATE INDEX IF NOT EXISTS idx_deployments_asset_id ON deployments (asset_id);
CREATE INDEX IF NOT EXISTS idx_deployments_package_id ON deployments (package_id);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments (status);

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_role_values CHECK (
        role IN ('admin', 'operator', 'viewer')
    )
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users (role);

CREATE TABLE IF NOT EXISTS documents (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title            TEXT NOT NULL,
    content_markdown TEXT NOT NULL DEFAULT '',
    uploaded_by      UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_documents_created_at ON documents (created_at DESC);

CREATE TABLE IF NOT EXISTS android_policies (
    asset_id                 UUID PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    camera_disabled          BOOLEAN NOT NULL DEFAULT false,
    screen_lock_timeout_ms   BIGINT NOT NULL DEFAULT 0,
    wipe_requested           BOOLEAN NOT NULL DEFAULT false,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_android_policies_updated_at ON android_policies (updated_at DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    logged_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id          UUID REFERENCES users (id) ON DELETE SET NULL,
    action           TEXT NOT NULL,
    target_asset_id  UUID REFERENCES assets (id) ON DELETE SET NULL,
    details_json     JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_logged_at ON audit_logs (logged_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_target_asset_id ON audit_logs (target_asset_id);

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

CREATE TABLE IF NOT EXISTS alert_stale_ack (
    asset_id   UUID PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    alerted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
