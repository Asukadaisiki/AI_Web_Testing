package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func markRuntimeCommandProcessed(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
) {
	t.Helper()
	var eventID string
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT event_id, payload
		FROM outbox_events
		WHERE key = $1 AND topic = $2`,
		runID, runCommandsTopic,
	).Scan(&eventID, &payload); err != nil {
		t.Fatalf("read command outbox for %s: %v", runID, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbox_events
			(id, consumer, event_id, payload, processed_at)
		VALUES ($1, 'agenttrace-runtime', $2, $3::jsonb, now())`,
		"inbox_delete_"+eventID, eventID, payload,
	); err != nil {
		t.Fatalf("mark runtime command processed for %s: %v", runID, err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO consumer_retry_counts
			(consumer, event_id, payload, attempt_count)
		VALUES ('agenttrace-runtime', $1, $2::jsonb, 1)`,
		eventID, payload,
	); err != nil {
		t.Fatalf("create consumer retry fixture for %s: %v", runID, err)
	}
}
