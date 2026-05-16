-- Phase 39: Compliance status, device quarantine flag, tenant auto-quarantine, quarantine C2 command type.

CREATE TABLE IF NOT EXISTS tenant_settings (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    auto_quarantine_on_noncompliance BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO tenant_settings (singleton, auto_quarantine_on_noncompliance)
VALUES (1, false)
ON CONFLICT (singleton) DO NOTHING;

ALTER TABLE assets
    ADD COLUMN IF NOT EXISTS compliance_status TEXT NOT NULL DEFAULT 'evaluating',
    ADD COLUMN IF NOT EXISTS compliance_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS quarantine_enabled BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE assets DROP CONSTRAINT IF EXISTS assets_compliance_status_chk;
ALTER TABLE assets ADD CONSTRAINT assets_compliance_status_chk CHECK (
    compliance_status IN ('compliant', 'non_compliant', 'evaluating')
);

ALTER TABLE device_commands DROP CONSTRAINT IF EXISTS device_commands_type_check;
ALTER TABLE device_commands ADD CONSTRAINT device_commands_type_check CHECK (
    command_type IN ('ping', 'reboot', 'script', 'restart_service', 'push_config', 'quarantine')
);

CREATE OR REPLACE VIEW devices AS
SELECT id,
       human_id,
       display_name,
       hostname,
       operational_status,
       cost_center,
       location,
       cert_serial,
       os_type,
       last_seen,
       metadata,
       compliance_status,
       compliance_reason,
       quarantine_enabled,
       created_at,
       updated_at
FROM assets;

COMMENT ON COLUMN assets.compliance_status IS 'Device posture: compliant, non_compliant, or evaluating.';
COMMENT ON COLUMN assets.quarantine_enabled IS 'Operator or automation toggled network/app isolation (quarantine) on this asset.';
