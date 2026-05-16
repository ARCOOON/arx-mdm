package ws

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadTicketSnapshot returns recent tickets for the operator dashboard.
func LoadTicketSnapshot(ctx context.Context, pool *pgxpool.Pool) ([]TicketWire, error) {
	if pool == nil {
		return []TicketWire{}, nil
	}
	rows, err := pool.Query(ctx, `
SELECT t.id, t.ticket_ref, t.title, t.status, t.priority, t.created_at, a.human_id
FROM tickets t
LEFT JOIN assets a ON a.id = t.device_id
ORDER BY t.created_at DESC
LIMIT 500
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TicketWire, 0)
	for rows.Next() {
		var t TicketWire
		var tid uuid.UUID
		var created time.Time
		var linked *string
		if err := rows.Scan(&tid, &t.TicketRef, &t.Title, &t.Status, &t.Priority, &created, &linked); err != nil {
			return nil, err
		}
		t.ID = tid.String()
		if linked != nil && *linked != "" {
			t.LinkedArxID = linked
		}
		t.CreatedAt = created.UTC().Format(time.RFC3339)
		out = append(out, t)
	}
	return out, rows.Err()
}
