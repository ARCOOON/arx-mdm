-- Phase 27: App catalog binaries and device deployment tracking.

CREATE TABLE app_catalog (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    target_os TEXT NOT NULL CHECK (target_os IN ('windows', 'linux', 'android')),
    file_path_or_url TEXT NOT NULL,
    install_args TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON COLUMN app_catalog.file_path_or_url IS
    'Server-local relative path produced by uploads (under ARX_APPS_STORAGE_ROOT) or HTTPS URL hosted elsewhere.';

CREATE TABLE device_apps (
    device_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    app_id UUID NOT NULL REFERENCES app_catalog(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'installing', 'success', 'failed')),
    last_updated TIMESTAMPTZ NOT NULL DEFAULT now(),
    error_message TEXT,
    PRIMARY KEY (device_id, app_id)
);

CREATE INDEX device_apps_app_id_idx ON device_apps(app_id);
