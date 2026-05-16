-- Phase 23: Audit log columns for RBAC / structured auditing (resource + IP + created_at).

ALTER TABLE audit_logs RENAME COLUMN logged_at TO created_at;
ALTER TABLE audit_logs RENAME COLUMN details_json TO details;

ALTER TABLE audit_logs ADD COLUMN resource_type TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN resource_id UUID;
ALTER TABLE audit_logs ADD COLUMN ip_address TEXT;

UPDATE audit_logs
SET resource_type = 'device',
    resource_id   = target_asset_id
WHERE resource_type = ''
  AND target_asset_id IS NOT NULL;

DROP INDEX IF EXISTS idx_audit_logs_logged_at;
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs (resource_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_id ON audit_logs (resource_id) WHERE resource_id IS NOT NULL;
