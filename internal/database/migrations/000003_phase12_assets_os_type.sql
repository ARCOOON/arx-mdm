-- Phase 12: explicit asset OS classification (includes android for MDM).
ALTER TABLE assets
    ADD COLUMN IF NOT EXISTS os_type TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE assets DROP CONSTRAINT IF EXISTS assets_os_type_values;

ALTER TABLE assets
    ADD CONSTRAINT assets_os_type_values CHECK (
        os_type IN ('unknown', 'windows', 'linux', 'darwin', 'android', 'ios')
    );
