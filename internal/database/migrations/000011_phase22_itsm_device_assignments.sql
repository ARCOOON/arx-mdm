-- Phase 22: ITSM ticket fields (device link, actors, description), priorityMedium, restricted status enum, device assignment rows.

CREATE TABLE IF NOT EXISTS device_assignments (
    asset_id UUID PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_assignments_user_id ON device_assignments (user_id);

-- Tickets: rename asset linkage to device_id (still references assets; devices are modeled as assets).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'tickets'
          AND column_name = 'asset_id'
    ) THEN
        ALTER TABLE tickets RENAME COLUMN asset_id TO device_id;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_tickets_asset_id;
CREATE INDEX IF NOT EXISTS idx_tickets_device_id ON tickets (device_id);

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users (id) ON DELETE SET NULL;

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS assigned_to UUID REFERENCES users (id) ON DELETE SET NULL;

-- Normalize legacy priorities: normal → medium per ITSM terminology.
UPDATE tickets SET priority = 'medium' WHERE lower(trim(priority)) = 'normal';

ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_priority_values;
ALTER TABLE tickets ADD CONSTRAINT tickets_priority_values CHECK (
    priority IN ('low', 'medium', 'high', 'critical')
);

-- Restrict ticket workflow states.
UPDATE tickets SET status = 'in_progress' WHERE lower(trim(status)) = 'pending';

ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_status_values;
ALTER TABLE tickets ADD CONSTRAINT tickets_status_values CHECK (
    status IN ('open', 'in_progress', 'resolved', 'closed')
);

-- Attach historical rows to earliest dashboard user where possible (nullable remains valid for orphaned rows).
UPDATE tickets AS t
SET created_by = u.id
FROM (SELECT id FROM users ORDER BY created_at ASC LIMIT 1) AS u
WHERE t.created_by IS NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_created_by ON tickets (created_by);
CREATE INDEX IF NOT EXISTS idx_tickets_assigned_to ON tickets (assigned_to);
