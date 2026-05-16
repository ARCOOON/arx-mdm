package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AssignedProfileRow is a flattened view for telemetry and agent synchronization.
type AssignedProfileRow struct {
	ID       uuid.UUID
	Name     string
	Type     string
	Payload  json.RawMessage
	Platform string
}

// ListAssignedProfilesForAsset returns distinct profiles assigned directly or transitively via principal groups.
func ListAssignedProfilesForAsset(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, platform string) ([]AssignedProfileRow, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool is required")
	}
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "" {
		return nil, fmt.Errorf("platform is required")
	}
	rows, err := pool.Query(ctx, `
SELECT DISTINCT cp.id, cp.name, cp.type, cp.payload, cp.platform
FROM configuration_profiles cp
JOIN profile_assignments pa ON pa.profile_id = cp.id
LEFT JOIN principal_group_devices pgd
       ON pa.target_kind = 'principal_group'
      AND pa.principal_group_id = pgd.group_id
      AND pgd.device_id = $1::uuid
WHERE lower(trim(cp.platform)) = $2
  AND (
        (pa.target_kind = 'device' AND pa.device_id = $1::uuid)
     OR (pa.target_kind = 'principal_group' AND pgd.device_id IS NOT NULL)
  )
ORDER BY cp.name ASC
`, assetID, p)
	if err != nil {
		return nil, fmt.Errorf("list assigned profiles: %w", err)
	}
	defer rows.Close()

	var out []AssignedProfileRow
	for rows.Next() {
		var r AssignedProfileRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Payload, &r.Platform); err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ManagedAndroidAppConfigWire is flattened for JSON delivery to telemetry clients.
type ManagedAndroidAppConfigWire struct {
	Package string         `json:"package_name"`
	KV      map[string]any `json:"managed_config_kv"`
}

// ListManagedAndroidConfigurationsForDevice returns deployed catalog App Config entries for Android targets.
func ListManagedAndroidConfigurationsForDevice(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID) ([]ManagedAndroidAppConfigWire, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool is required")
	}
	rows, err := pool.Query(ctx, `
SELECT ac.managed_package_name, COALESCE(ac.config_kv, '{}'::jsonb)::text
FROM app_configurations ac
JOIN device_apps da ON da.app_id = ac.catalog_app_id
JOIN app_catalog cat ON cat.id = ac.catalog_app_id
WHERE da.device_id = $1::uuid
  AND lower(trim(cat.target_os)) = 'android'
  AND da.status IN ('pending', 'installing', 'success')
ORDER BY ac.managed_package_name ASC
`, assetID)
	if err != nil {
		return nil, fmt.Errorf("list managed app configs: %w", err)
	}
	defer rows.Close()

	var out []ManagedAndroidAppConfigWire
	for rows.Next() {
		var pkg, rawKV string
		if err := rows.Scan(&pkg, &rawKV); err != nil {
			return nil, fmt.Errorf("scan app config: %w", err)
		}
		var kv map[string]any
		if err := json.Unmarshal([]byte(rawKV), &kv); err != nil {
			kv = map[string]any{}
		}
		out = append(out, ManagedAndroidAppConfigWire{Package: pkg, KV: kv})
	}
	return out, rows.Err()
}

func appendProfileWIRE(out []AssignedProfileRow) ([]map[string]any, error) {
	wires := make([]map[string]any, 0, len(out))
	for _, r := range out {
		var payload any
		if len(r.Payload) == 0 {
			payload = map[string]any{}
		} else if err := json.Unmarshal(r.Payload, &payload); err != nil {
			return nil, fmt.Errorf("profile payload decode: %w", err)
		}
		wires = append(wires, map[string]any{
			"id":           r.ID.String(),
			"name":         r.Name,
			"profile_type": r.Type,
			"platform":     r.Platform,
			"payload":      payload,
		})
	}
	return wires, nil
}

// BuildMDMProfilesWireForAsset composes telemetry JSON fragments for ProcessTelemetry handlers.
func BuildMDMProfilesWireForAsset(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, platform string) (profiles []map[string]any, androidManaged []ManagedAndroidAppConfigWire, err error) {
	plist, err := ListAssignedProfilesForAsset(ctx, pool, assetID, platform)
	if err != nil {
		return nil, nil, err
	}
	profiles, err = appendProfileWIRE(plist)
	if err != nil {
		return nil, nil, err
	}
	if strings.EqualFold(strings.TrimSpace(platform), "android") {
		androidManaged, err = ListManagedAndroidConfigurationsForDevice(ctx, pool, assetID)
		if err != nil {
			return nil, nil, err
		}
	}
	return profiles, androidManaged, nil
}
