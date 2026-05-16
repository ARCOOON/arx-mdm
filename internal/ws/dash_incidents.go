package ws

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadIncidentSnapshot returns recent incidents for the operator dashboard.
func LoadIncidentSnapshot(ctx context.Context, pool *pgxpool.Pool) ([]IncidentWire, error) {
	if pool == nil {
		return []IncidentWire{}, nil
	}
	rows, err := pool.Query(ctx, `
SELECT i.id, i.incident_number, i.short_description, i.state, i.priority,
       i.sla_due, i.created_at, a.human_id
FROM incidents i
LEFT JOIN assets a ON a.id = i.cmdb_ci
ORDER BY i.created_at DESC
LIMIT 500
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]IncidentWire, 0)
	for rows.Next() {
		var w IncidentWire
		var tid uuid.UUID
		var linked *string
		var sla time.Time
		var createdAt time.Time
		if err := rows.Scan(
			&tid, &w.IncidentNumber, &w.ShortDescription, &w.State, &w.Priority,
			&sla, &createdAt, &linked,
		); err != nil {
			return nil, err
		}
		w.ID = tid.String()
		w.SLADue = sla.UTC().Format(time.RFC3339)
		w.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if linked != nil && strings.TrimSpace(*linked) != "" {
			h := strings.TrimSpace(*linked)
			w.LinkedArxID = &h
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
