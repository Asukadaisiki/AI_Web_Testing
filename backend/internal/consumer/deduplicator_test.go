package consumer_test

import (
	"context"
	"os"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeduplicatorRunsHandlerOnceForDuplicateEventID(t *testing.T) {
	ctx, pool := newConsumerTestPool(t)
	deduplicator := consumer.NewDeduplicator(pool, "runtime")

	calls := 0
	handler := func(ctx context.Context, tx consumer.Tx, message eventbus.Message) error {
		calls++
		_, err := tx.Exec(ctx, `
			INSERT INTO projects (id, name) VALUES ('prj_effect', 'effect')
			ON CONFLICT (id) DO NOTHING`)
		return err
	}
	message := eventbus.Message{
		Topic: "agenttrace.run.commands.v1",
		Key:   "run_123",
		Value: []byte(`{
			"schema_version":"agenttrace.event.v1",
			"event_id":"evt_duplicate",
			"event_type":"run.requested",
			"aggregate_id":"run_123",
			"occurred_at":"2026-10-21T12:00:00Z",
			"correlation_id":"run_123",
			"payload":{"run_id":"run_123"}
		}`),
	}

	if err := deduplicator.Handle(ctx, message, handler); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := deduplicator.Handle(ctx, message, handler); err != nil {
		t.Fatalf("duplicate handle: %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}

	var inboxRows, effects int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox_events WHERE consumer = 'runtime' AND event_id = 'evt_duplicate'`).Scan(&inboxRows); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM projects WHERE id = 'prj_effect'`).Scan(&effects); err != nil {
		t.Fatalf("count durable effects: %v", err)
	}
	if inboxRows != 1 || effects != 1 {
		t.Fatalf("inbox rows=%d effects=%d, want one each", inboxRows, effects)
	}
}

func newConsumerTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset test database: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return ctx, pool
}
