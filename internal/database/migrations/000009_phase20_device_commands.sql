-- Phase 20: Device command queue for C2 over WebSockets.

CREATE TABLE IF NOT EXISTS device_commands (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    command_type  TEXT NOT NULL,
    payload       TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending',
    output        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    CONSTRAINT device_commands_type_check CHECK (
        command_type IN ('ping', 'reboot', 'script')
    ),
    CONSTRAINT device_commands_status_check CHECK (
        status IN ('pending', 'sent', 'completed', 'failed')
    )
);

CREATE INDEX IF NOT EXISTS idx_device_commands_device_created
    ON device_commands (device_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_device_commands_status
    ON device_commands (status)
    WHERE status IN ('pending', 'sent');
