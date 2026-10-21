package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRetryStoreHandsWhitespaceEventIDToDLQ(t *testing.T) {
	ctx, pool, store := newRuntimePostgresStore(t)
	runner := &recordingRunner{err: errors.New("permanent failure")}
	retries := PostgresRetryStore{Store: store, Consumer: "runtime"}
	message := commandMessage(t, " \t ", commandFor("run-postgres-whitespace-dlq"))
	retryID := stableID("consumer-retry", message.Topic, message.Key, string(message.Value))
	outboxID := stableID("consumer-dlq", retryID)

	first := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := first.Handle(ctx, message); err == nil {
		t.Fatal("first whitespace event ID failure returned nil")
	}
	second := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := second.Handle(ctx, message); err != nil {
		t.Fatalf("durable whitespace event ID handoff: %v", err)
	}

	var (
		retryRows int
		topic     string
		key       string
		payload   []byte
	)
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM consumer_retry_counts
		WHERE consumer = 'runtime' AND event_id = $1`, retryID).Scan(&retryRows); err != nil {
		t.Fatalf("count retry state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT topic, key, payload
		FROM outbox_events WHERE event_id = $1`, outboxID).Scan(&topic, &key, &payload); err != nil {
		t.Fatalf("load DLQ outbox event: %v", err)
	}
	if retryRows != 0 {
		t.Fatalf("retry rows = %d, want 0 after handoff", retryRows)
	}
	if topic != defaultDLQTopic || key != message.Key {
		t.Fatalf("DLQ route = (%q, %q), want (%q, %q)", topic, key, defaultDLQTopic, message.Key)
	}
	var dlq map[string]json.RawMessage
	if err := json.Unmarshal(payload, &dlq); err != nil {
		t.Fatalf("decode DLQ payload: %v", err)
	}
	var original map[string]any
	if err := json.Unmarshal(dlq["original"], &original); err != nil {
		t.Fatalf("decode original DLQ payload: %v", err)
	}
	if original["event_id"] != " \t " {
		t.Fatalf("DLQ original event_id = %#v, want whitespace source value", original["event_id"])
	}
}

func TestPostgresRetryStoreCreatesOneDLQHandoffForConcurrentConsumersAndReplay(t *testing.T) {
	ctx, pool, store := newRuntimePostgresStore(t)
	runner := &recordingRunner{err: errors.New("permanent failure")}
	retries := PostgresRetryStore{Store: store, Consumer: "runtime"}
	message := commandMessage(t, "evt-postgres-concurrent-dlq", commandFor("run-postgres-concurrent-dlq"))
	retryID := retryEventID("evt-postgres-concurrent-dlq", message)
	outboxID := stableID("consumer-dlq", retryID)
	start := make(chan struct{})
	errs := make(chan error, 2)

	for range 2 {
		consumer := Consumer{
			Inbox:       &memoryInbox{},
			Runner:      runner,
			RetryStore:  retries,
			MaxAttempts: 1,
		}
		go func() {
			<-start
			errs <- consumer.Handle(ctx, message)
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent durable DLQ handoff: %v", err)
		}
	}

	var retryRows, outboxRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM consumer_retry_counts
		WHERE consumer = 'runtime' AND event_id = $1`, retryID).Scan(&retryRows); err != nil {
		t.Fatalf("count retry state: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event_id = $1 AND topic = $2 AND key = $3`,
		outboxID, defaultDLQTopic, message.Key,
	).Scan(&outboxRows); err != nil {
		t.Fatalf("count DLQ outbox event: %v", err)
	}
	if retryRows != 0 || outboxRows != 1 {
		t.Fatalf("retry rows=%d outbox rows=%d, want 0 and 1", retryRows, outboxRows)
	}

	callsBeforeReplay := runner.calls
	replay := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 1,
	}
	if err := replay.Handle(ctx, message); err != nil {
		t.Fatalf("replay after durable DLQ handoff: %v", err)
	}
	if runner.calls != callsBeforeReplay {
		t.Fatalf("runner calls after replay = %d, want %d", runner.calls, callsBeforeReplay)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events WHERE event_id = $1`, outboxID).Scan(&outboxRows); err != nil {
		t.Fatalf("count replayed DLQ outbox event: %v", err)
	}
	if outboxRows != 1 {
		t.Fatalf("DLQ outbox rows after replay = %d, want one", outboxRows)
	}
}

func newRuntimePostgresStore(t *testing.T) (context.Context, *pgxpool.Pool, *postgres.Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset PostgreSQL: %v", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	return ctx, pool, postgres.NewStore(pool)
}
