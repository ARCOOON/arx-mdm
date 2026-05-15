-- Phase 14: Audit trail for dashboard REST mutations and C&C commands.
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
