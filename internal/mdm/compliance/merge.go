package compliance

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ARCOOON/arx-mdm/internal/database"
	"github.com/ARCOOON/arx-mdm/internal/mdm/policy"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assignedInputs(rows []database.AssignedProfileRow) []policy.AssignedPayload {
	out := make([]policy.AssignedPayload, 0, len(rows))
	for _, r := range rows {
		src := policy.ProfileSource{ID: r.ID, Name: r.Name}
		raw := r.Payload
		if raw == nil || len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage([]byte("{}"))
		}
		out = append(out, policy.AssignedPayload{Source: src, Payload: raw})
	}
	return out
}

// EffectiveMergedPayload returns merged declarative JSON for an asset without reconciling assignment rows.
func EffectiveMergedPayload(ctx context.Context, pool *pgxpool.Pool, assetID uuid.UUID, platformKey string) (map[string]any, error) {
	if pool == nil {
		return nil, ErrNilPool
	}
	platformKey = strings.ToLower(strings.TrimSpace(platformKey))
	rows, err := database.ListAssignedProfilesForAsset(ctx, pool, assetID, platformKey)
	if err != nil {
		return nil, err
	}
	mr, err := policy.MergeAssignedPayloads(assignedInputs(rows))
	if err != nil {
		return nil, err
	}
	if mr == nil || mr.EffectivePayload == nil {
		return map[string]any{}, nil
	}
	return mr.EffectivePayload, nil
}
