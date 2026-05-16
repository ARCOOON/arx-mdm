-- Phase 36: ITSM incidents table (replacing legacy tickets), CMDB columns on assets, device command linkage.

ALTER TABLE assets
    ADD COLUMN IF NOT EXISTS operational_status TEXT NOT NULL DEFAULT 'operational',
    ADD COLUMN IF NOT EXISTS cost_center TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT '';

ALTER TABLE assets DROP CONSTRAINT IF EXISTS assets_operational_status_chk;
ALTER TABLE assets ADD CONSTRAINT assets_operational_status_chk CHECK (
    operational_status IN ('operational', 'maintenance', 'retired', 'stock')
);

ALTER TABLE resolutions DROP CONSTRAINT IF EXISTS resolutions_ticket_id_fkey;

ALTER TABLE IF EXISTS tickets RENAME TO incidents;

ALTER INDEX IF EXISTS idx_tickets_device_id RENAME TO idx_incidents_cmdb_ci;
ALTER INDEX IF EXISTS idx_tickets_status RENAME TO idx_incidents_state;
ALTER INDEX IF EXISTS idx_tickets_created_by RENAME TO idx_incidents_caller_id;
ALTER INDEX IF EXISTS idx_tickets_assigned_to RENAME TO idx_incidents_assigned_to;

ALTER TABLE resolutions RENAME COLUMN ticket_id TO incident_id;
ALTER INDEX IF EXISTS idx_resolutions_ticket_id RENAME TO idx_resolutions_incident_id;

ALTER TABLE resolutions
    ADD CONSTRAINT resolutions_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE;

ALTER TABLE incidents DROP CONSTRAINT IF EXISTS tickets_ref_prefix;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS tickets_priority_values;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS tickets_status_values;

ALTER TABLE incidents RENAME COLUMN ticket_ref TO incident_number;
ALTER TABLE incidents RENAME COLUMN title TO short_description;
ALTER TABLE incidents RENAME COLUMN device_id TO cmdb_ci;
ALTER TABLE incidents RENAME COLUMN created_by TO caller_id;
ALTER TABLE incidents RENAME COLUMN status TO legacy_state_txt;

CREATE SEQUENCE IF NOT EXISTS incident_seq AS bigint START 1 INCREMENT 1;

UPDATE incidents
SET incident_number = 'INC' || LPAD(split_part(trim(lower(incident_number)), '-', 2), 7, '0')
WHERE incident_number ~* '^INC-[0-9]+$';

UPDATE incidents
SET incident_number = 'INC' || LPAD(nextval('incident_seq'::regclass)::text, 7, '0')
WHERE incident_number !~ '^INC[0-9]{7}$';

SELECT setval(
    'incident_seq'::regclass,
    GREATEST(
        COALESCE(
            (
                SELECT MAX(substring(incident_number FROM 4)::bigint)
                FROM incidents
                WHERE incident_number ~ '^INC[0-9]{7}$'
            ),
            0::bigint
        ),
        1::bigint
    )
);

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS work_notes JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE incidents
SET work_notes = COALESCE(work_notes, '[]'::jsonb)
    || jsonb_build_array(
        jsonb_build_object(
            'ts', to_jsonb(now()),
            'author_type', 'system',
            'kind', 'legacy_description_import',
            'text', left(coalesce(trim(description), ''), 65536)
        )
    )
WHERE trim(coalesce(description, '')) <> '';

ALTER TABLE incidents DROP COLUMN IF EXISTS description;

UPDATE incidents SET legacy_state_txt = CASE lower(trim(legacy_state_txt))
    WHEN 'open' THEN 'new'
    ELSE lower(trim(legacy_state_txt))
END;

ALTER TABLE incidents RENAME COLUMN legacy_state_txt TO state;

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS impact SMALLINT NOT NULL DEFAULT 2 CONSTRAINT incidents_impact_chk CHECK (impact IN (1, 2, 3));
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS urgency SMALLINT NOT NULL DEFAULT 2 CONSTRAINT incidents_urgency_chk CHECK (urgency IN (1, 2, 3));

UPDATE incidents SET impact = CASE lower(trim(priority))
        WHEN 'critical' THEN 1::smallint
        WHEN 'high' THEN 1::smallint
        WHEN 'medium' THEN 2::smallint
        ELSE 3::smallint
    END;

UPDATE incidents SET urgency = CASE lower(trim(priority))
        WHEN 'critical' THEN 1::smallint
        WHEN 'high' THEN 2::smallint
        WHEN 'medium' THEN 2::smallint
        ELSE 3::smallint
    END;

ALTER TABLE incidents DROP COLUMN IF EXISTS priority;

CREATE OR REPLACE FUNCTION incident_priority_rank(impact smallint, urgency smallint)
RETURNS smallint
LANGUAGE sql
IMMUTABLE AS
$$
SELECT (
    CASE COALESCE(impact, 2)::int * 10 + COALESCE(urgency, 2)::int
        WHEN 11 THEN 1
        WHEN 12 THEN 2
        WHEN 13 THEN 3
        WHEN 21 THEN 2
        WHEN 22 THEN 3
        WHEN 23 THEN 4
        WHEN 31 THEN 3
        WHEN 32 THEN 4
        WHEN 33 THEN 5
        ELSE 3
        END
    )::smallint
$$;

ALTER TABLE incidents ADD COLUMN priority smallint GENERATED ALWAYS AS (
    incident_priority_rank(impact, urgency)
) STORED;

ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_state_chk;
ALTER TABLE incidents ADD CONSTRAINT incidents_state_chk CHECK (
    state IN ('new', 'in_progress', 'on_hold', 'resolved', 'closed')
    );

ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_number_fmt;
ALTER TABLE incidents ADD CONSTRAINT incidents_number_fmt CHECK (incident_number ~ '^INC[0-9]{7}$');

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS source_alert_fingerprint TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_source_alert_fp
    ON incidents (source_alert_fingerprint)
    WHERE source_alert_fingerprint IS NOT NULL AND trim(source_alert_fingerprint) <> '';

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS sla_due TIMESTAMPTZ;

UPDATE incidents
SET sla_due = COALESCE(sla_due, created_at + interval '4 hours')
WHERE sla_due IS NULL;

ALTER TABLE incidents ALTER COLUMN sla_due SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_incidents_sla_running
    ON incidents (sla_due ASC)
    WHERE state NOT IN ('resolved', 'closed');

ALTER TABLE device_commands ADD COLUMN IF NOT EXISTS incident_context_id UUID REFERENCES incidents (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_device_commands_incident ON device_commands (incident_context_id)
    WHERE incident_context_id IS NOT NULL;

ALTER TABLE device_commands DROP CONSTRAINT IF EXISTS device_commands_type_check;
ALTER TABLE device_commands ADD CONSTRAINT device_commands_type_check CHECK (
    command_type IN ('ping', 'reboot', 'script', 'restart_service', 'push_config')
);

DROP SEQUENCE IF EXISTS ticket_seq_req;
DROP SEQUENCE IF EXISTS ticket_seq_chg;
DROP SEQUENCE IF EXISTS ticket_seq_prj;
DROP SEQUENCE IF EXISTS ticket_seq_inc;

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
       created_at,
       updated_at
FROM assets;

COMMENT ON VIEW devices IS 'CMDB device projection over assets table; incidents.cmdb_ci references assets.id.';
