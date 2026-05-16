package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppendMDMDownlink injects declarative payloads into successful telemetry acknowledgement maps.
func AppendMDMDownlink(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, osType string, resp map[string]any) error {
	if pool == nil || resp == nil {
		return nil
	}
	platformKey := telemetryPlatformKey(osType)
	profilesWire, managed, err := database.BuildMDMProfilesWireForAsset(ctx, pool, assetID, platformKey)
	if err != nil {
		return fmt.Errorf("mdm downlink assemble: %w", err)
	}
	if len(profilesWire) > 0 {
		resp["mdm_configuration_profiles"] = profilesWire
		rev, rerr := profileRevisionStable(profilesWire)
		if rerr != nil {
			return rerr
		}
		resp["mdm_profile_revision"] = rev
	}
	if len(managed) > 0 {
		resp["mdm_managed_app_configs"] = managed
	}
	return nil
}

func telemetryPlatformKey(osType string) string {
	return strings.ToLower(strings.TrimSpace(osType))
}

func profileRevisionStable(wires []map[string]any) (string, error) {
	payload, err := json.Marshal(wires)
	if err != nil {
		return "", fmt.Errorf("mdm profile revision: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
