-- Phase 21: Time-series device telemetry (CPU, RAM, disk) for dashboards.

CREATE TABLE IF NOT EXISTS device_metrics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id   UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    cpu_usage   DOUBLE PRECISION NOT NULL,
    ram_total   BIGINT NOT NULL,
    ram_used    BIGINT NOT NULL,
    disk_total  BIGINT NOT NULL,
    disk_used   BIGINT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_device_metrics_device_created
    ON device_metrics (device_id, created_at DESC);
