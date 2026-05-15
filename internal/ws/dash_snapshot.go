package ws

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type assetRow struct {
	ID         uuid.UUID
	HumanID    string
	Hostname   *string
	CertSerial *string
	OsType     string
	LastSeen   *time.Time
	Metadata   []byte
}

// LoadAssetSnapshot queries the database and builds dashboard asset rows.
func LoadAssetSnapshot(ctx context.Context, pool *pgxpool.Pool, c2 *Hub) ([]AssetWire, error) {
	if pool == nil {
		return []AssetWire{}, nil
	}
	rows, err := pool.Query(ctx, `
SELECT id, human_id, hostname, cert_serial, os_type, last_seen, COALESCE(metadata, '{}'::jsonb)::text
FROM assets
ORDER BY human_id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	connected := map[string]struct{}{}
	if c2 != nil {
		for _, s := range c2.ConnectedCertSerials() {
			connected[strings.TrimSpace(s)] = struct{}{}
		}
	}

	out := make([]AssetWire, 0)
	for rows.Next() {
		var r assetRow
		if err := rows.Scan(&r.ID, &r.HumanID, &r.Hostname, &r.CertSerial, &r.OsType, &r.LastSeen, &r.Metadata); err != nil {
			return nil, err
		}
		out = append(out, assetWireFromDB(r, connected))
	}
	return out, rows.Err()
}

func assetWireFromDB(r assetRow, connectedSerial map[string]struct{}) AssetWire {
	host := ""
	if r.Hostname != nil {
		host = strings.TrimSpace(*r.Hostname)
	}
	serial := ""
	if r.CertSerial != nil {
		serial = strings.TrimSpace(*r.CertSerial)
	}
	_, c2On := connectedSerial[serial]

	var meta struct {
		Telemetry *struct {
			OSFamily            *string                     `json:"os_family"`
			OSVersion           *string                     `json:"os_version"`
			CPUModel            *string                     `json:"cpu_model"`
			CPULogicalCores     *int                        `json:"cpu_logical_cores"`
			CPUUsagePercent     *float64                    `json:"cpu_usage_percent"`
			TotalRAMBytes       *uint64                    `json:"total_ram_bytes"`
			MemoryUsedBytes     *uint64                    `json:"memory_used_bytes"`
			ReportedHostname    *string                     `json:"reported_hostname"`
			ReportedAtRFC3339   *string                     `json:"reported_at_rfc3339"`
			InstalledSoftware   []api.TelemetryInstalledApp `json:"installed_software"`
		} `json:"telemetry"`
	}
	_ = json.Unmarshal(r.Metadata, &meta)

	a := AssetWire{
		ID:          r.ID.String(),
		HumanID:     r.HumanID,
		OsType:      strings.TrimSpace(r.OsType),
		C2Connected: c2On,
	}
	if meta.Telemetry != nil {
		t := meta.Telemetry
		if t.OSFamily != nil {
			a.OS = strings.TrimSpace(*t.OSFamily)
		}
		if t.OSVersion != nil {
			a.OS = strings.TrimSpace(strings.TrimSpace(a.OS) + " " + strings.TrimSpace(*t.OSVersion))
		}
		if t.CPUModel != nil {
			a.CPUModel = strings.TrimSpace(*t.CPUModel)
		}
		if t.CPULogicalCores != nil {
			a.CPULogicalCores = *t.CPULogicalCores
		}
		if t.CPUUsagePercent != nil {
			a.CPUUsagePercent = *t.CPUUsagePercent
		}
		if t.TotalRAMBytes != nil {
			a.TotalRAMBytes = *t.TotalRAMBytes
		}
		if t.MemoryUsedBytes != nil {
			a.MemoryUsedBytes = *t.MemoryUsedBytes
		}
		if t.ReportedAtRFC3339 != nil {
			a.LastSeenRFC3339 = strings.TrimSpace(*t.ReportedAtRFC3339)
		}
		if t.InstalledSoftware != nil {
			a.InstalledSoftware = t.InstalledSoftware
		}
	}
	if meta.Telemetry != nil && meta.Telemetry.ReportedHostname != nil && strings.TrimSpace(*meta.Telemetry.ReportedHostname) != "" {
		a.Hostname = strings.TrimSpace(*meta.Telemetry.ReportedHostname)
	} else {
		a.Hostname = host
	}
	if r.LastSeen != nil && a.LastSeenRFC3339 == "" {
		a.LastSeenRFC3339 = r.LastSeen.UTC().Format(time.RFC3339)
	}
	return a
}
