-- Phase 13: Android DPC policy state (camera, lock timeout, remote wipe latch).
CREATE TABLE IF NOT EXISTS android_policies (
    asset_id                 UUID PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    camera_disabled          BOOLEAN NOT NULL DEFAULT false,
    screen_lock_timeout_ms   BIGINT NOT NULL DEFAULT 0,
    wipe_requested           BOOLEAN NOT NULL DEFAULT false,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_android_policies_updated_at ON android_policies (updated_at DESC);
