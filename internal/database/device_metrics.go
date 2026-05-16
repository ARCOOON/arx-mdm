package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	deviceMetricsMaxHours      = 168
	deviceMetricsMaxPoints     = 360
	deviceMetricsMinBucketSecs = 60
)

// InsertDeviceMetric appends one telemetry row for the given device (asset).
func InsertDeviceMetric(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID, cpuUsage float64, ramTotal, ramUsed, diskTotal, diskUsed int64) error {
	if pool == nil {
		return errors.New("database: pool is required")
	}
	cpuUsage = sanitizeTelemetryFloat(cpuUsage)
	ramTotal = nonNegativeInt64(ramTotal)
	ramUsed = nonNegativeInt64(ramUsed)
	diskTotal = nonNegativeInt64(diskTotal)
	diskUsed = nonNegativeInt64(diskUsed)
	if diskUsed > diskTotal && diskTotal > 0 {
		diskUsed = diskTotal
	}
	if ramUsed > ramTotal && ramTotal > 0 {
		ramUsed = ramTotal
	}
	_, err := pool.Exec(ctx, `
INSERT INTO device_metrics (device_id, cpu_usage, ram_total, ram_used, disk_total, disk_used)
VALUES ($1, $2, $3, $4, $5, $6)
`, deviceID, cpuUsage, ramTotal, ramUsed, diskTotal, diskUsed)
	if err != nil {
		return fmt.Errorf("database: insert device_metrics: %w", err)
	}
	return nil
}

// DeviceMetricSeriesPoint is one aggregated bucket for charting.
type DeviceMetricSeriesPoint struct {
	T              time.Time `json:"t"`
	CPUUsage       float64   `json:"cpu_usage"`
	RAMUsedPercent float64   `json:"ram_used_percent"`
}

// DeviceMetricDiskSample is the most recent disk usage in the requested window.
type DeviceMetricDiskSample struct {
	TotalBytes int64     `json:"total_bytes"`
	UsedBytes  int64     `json:"used_bytes"`
	SampledAt  time.Time `json:"sampled_at"`
}

// DeviceMetricHistory is the REST response body for historical metrics.
type DeviceMetricHistory struct {
	DeviceID      uuid.UUID                 `json:"device_id"`
	Hours         int                       `json:"hours"`
	BucketSeconds int                       `json:"bucket_seconds"`
	Series        []DeviceMetricSeriesPoint `json:"series"`
	Disk          *DeviceMetricDiskSample   `json:"disk,omitempty"`
}

// LoadDeviceMetricHistory loads downsampled time series and latest disk totals for charts.
func LoadDeviceMetricHistory(ctx context.Context, pool *pgxpool.Pool, deviceID uuid.UUID, hours int) (DeviceMetricHistory, error) {
	var zero DeviceMetricHistory
	if pool == nil {
		return zero, errors.New("database: pool is required")
	}
	if hours < 1 {
		hours = 24
	}
	if hours > deviceMetricsMaxHours {
		hours = deviceMetricsMaxHours
	}
	bucket := (hours * 3600) / deviceMetricsMaxPoints
	if bucket < deviceMetricsMinBucketSecs {
		bucket = deviceMetricsMinBucketSecs
	}
	cutoff := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	rows, err := pool.Query(ctx, `
SELECT
  to_timestamp(floor(extract(epoch from created_at) / $1::float8) * $1::float8) AS bucket_start,
  avg(cpu_usage)::float8 AS cpu_usage,
  COALESCE(
    avg(CASE WHEN ram_total > 0 THEN 100.0 * ram_used::float8 / ram_total::float8 END),
    0
  )::float8 AS ram_used_percent
FROM device_metrics
WHERE device_id = $2 AND created_at >= $3
GROUP BY 1
ORDER BY 1 ASC
`, float64(bucket), deviceID, cutoff)
	if err != nil {
		return zero, fmt.Errorf("database: query device_metrics series: %w", err)
	}
	defer rows.Close()

	var series []DeviceMetricSeriesPoint
	for rows.Next() {
		var p DeviceMetricSeriesPoint
		if err := rows.Scan(&p.T, &p.CPUUsage, &p.RAMUsedPercent); err != nil {
			return zero, fmt.Errorf("database: scan device_metrics series: %w", err)
		}
		p.CPUUsage = sanitizeTelemetryFloat(p.CPUUsage)
		if math.IsNaN(p.RAMUsedPercent) || math.IsInf(p.RAMUsedPercent, 0) {
			p.RAMUsedPercent = 0
		}
		if p.RAMUsedPercent < 0 {
			p.RAMUsedPercent = 0
		}
		if p.RAMUsedPercent > 100 {
			p.RAMUsedPercent = 100
		}
		series = append(series, p)
	}
	if err := rows.Err(); err != nil {
		return zero, fmt.Errorf("database: iterate device_metrics series: %w", err)
	}

	var disk *DeviceMetricDiskSample
	var dt, du int64
	var at time.Time
	err = pool.QueryRow(ctx, `
SELECT disk_total, disk_used, created_at
FROM device_metrics
WHERE device_id = $1 AND created_at >= $2
ORDER BY created_at DESC
LIMIT 1
`, deviceID, cutoff).Scan(&dt, &du, &at)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("database: query device_metrics disk: %w", err)
	}
	if err == nil && (dt > 0 || du > 0) {
		if du > dt && dt > 0 {
			du = dt
		}
		disk = &DeviceMetricDiskSample{
			TotalBytes: dt,
			UsedBytes:  du,
			SampledAt:  at.UTC(),
		}
	}

	return DeviceMetricHistory{
		DeviceID:      deviceID,
		Hours:         hours,
		BucketSeconds: bucket,
		Series:        series,
		Disk:          disk,
	}, nil
}

func sanitizeTelemetryFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func nonNegativeInt64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
