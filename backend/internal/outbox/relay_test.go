package outbox_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRelayMarksPublishedOnlyAfterBrokerAck(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_ack", "evt_ack", nil)

	bus := eventbus.NewFake()
	bus.NextMetadata = eventbus.Metadata{Partition: 3, Offset: 42}
	relay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:      "relay-1",
		BatchSize:     10,
		LeaseDuration: time.Minute,
	})

	published, err := relay.RelayOnce(ctx)
	if err != nil {
		t.Fatalf("relay once: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}

	messages := bus.Published()
	if len(messages) != 1 {
		t.Fatalf("published messages = %d, want 1", len(messages))
	}
	if messages[0].Topic != "agenttrace.run.commands.v1" || messages[0].Key != "run_123" {
		t.Fatalf("published message = %#v", messages[0])
	}

	var (
		publishedAt *time.Time
		partition   *int32
		offset      *int64
		lockedBy    *string
	)
	if err := pool.QueryRow(ctx, `
		SELECT published_at, broker_partition, broker_offset, locked_by
		FROM outbox_events WHERE id = 'obx_ack'`,
	).Scan(&publishedAt, &partition, &offset, &lockedBy); err != nil {
		t.Fatalf("read published outbox event: %v", err)
	}
	if publishedAt == nil || partition == nil || *partition != 3 || offset == nil || *offset != 42 || lockedBy != nil {
		t.Fatalf("outbox publish state = published=%v partition=%v offset=%v locked_by=%v", publishedAt, partition, offset, lockedBy)
	}
}

func TestRelayLeavesRowPendingWhenPublishFails(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_fail", "evt_fail", nil)

	bus := eventbus.NewFake()
	bus.PublishErr = errors.New("broker unavailable")
	relay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:       "relay-1",
		BatchSize:      10,
		LeaseDuration:  time.Minute,
		InitialBackoff: time.Minute,
	})

	if _, err := relay.RelayOnce(ctx); err == nil {
		t.Fatal("expected publish failure")
	}
	if messages := bus.Published(); len(messages) != 1 || messages[0].Key != "run_123" {
		t.Fatalf("failed publish attempt = %#v, want recorded run message", messages)
	}

	var (
		publishedAt   *time.Time
		lockedBy      *string
		lockedUntil   *time.Time
		attemptCount  int
		nextAttemptAt time.Time
		lastError     *string
	)
	if err := pool.QueryRow(ctx, `
		SELECT published_at, locked_by, locked_until, attempt_count, next_attempt_at, last_error
		FROM outbox_events WHERE id = 'obx_fail'`,
	).Scan(&publishedAt, &lockedBy, &lockedUntil, &attemptCount, &nextAttemptAt, &lastError); err != nil {
		t.Fatalf("read failed outbox event: %v", err)
	}
	if publishedAt != nil || lockedBy != nil || lockedUntil != nil {
		t.Fatalf("failed event must be pending, got published=%v locked_by=%v locked_until=%v", publishedAt, lockedBy, lockedUntil)
	}
	if attemptCount != 1 || lastError == nil || *lastError != "broker unavailable" {
		t.Fatalf("failure state = attempts=%d error=%v", attemptCount, lastError)
	}
	if !nextAttemptAt.After(time.Now().Add(30 * time.Second)) {
		t.Fatalf("next attempt %s was not scheduled with backoff", nextAttemptAt)
	}
}

func TestRelayIncreasesRetryBackoffAfterEachFailure(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_backoff", "evt_backoff", nil)

	bus := eventbus.NewFake()
	bus.PublishErr = errors.New("broker unavailable")
	relay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:       "relay-1",
		BatchSize:      1,
		LeaseDuration:  time.Minute,
		InitialBackoff: time.Second,
	})

	if _, err := relay.RelayOnce(ctx); err == nil {
		t.Fatal("expected first publish failure")
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET next_attempt_at = now() WHERE id = 'obx_backoff'`); err != nil {
		t.Fatalf("make outbox event due: %v", err)
	}
	if _, err := relay.RelayOnce(ctx); err == nil {
		t.Fatal("expected second publish failure")
	}

	var nextAttemptAt time.Time
	var attempts int
	if err := pool.QueryRow(ctx, `
		SELECT next_attempt_at, attempt_count
		FROM outbox_events WHERE id = 'obx_backoff'`,
	).Scan(&nextAttemptAt, &attempts); err != nil {
		t.Fatalf("read retry state: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempt count = %d, want 2", attempts)
	}
	if !nextAttemptAt.After(time.Now().Add(1500 * time.Millisecond)) {
		t.Fatalf("second retry was not exponentially delayed: %s", nextAttemptAt)
	}
}

func TestRelayReclaimsExpiredLease(t *testing.T) {
	ctx, pool := newTestPool(t)
	expired := time.Now().Add(-time.Minute)
	insertOutboxEvent(t, ctx, pool, "obx_expired", "evt_expired", &expired)

	bus := eventbus.NewFake()
	relay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:      "relay-new",
		BatchSize:     10,
		LeaseDuration: time.Minute,
	})
	if published, err := relay.RelayOnce(ctx); err != nil || published != 1 {
		t.Fatalf("relay expired lease = (published=%d, err=%v), want (1, nil)", published, err)
	}

	var publishedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_events WHERE id = 'obx_expired'`).Scan(&publishedAt); err != nil {
		t.Fatalf("read reclaimed event: %v", err)
	}
	if publishedAt == nil {
		t.Fatal("expired lease event was not reclaimed and published")
	}
}

func newTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
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

func insertOutboxEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, eventID string, lockedUntil *time.Time) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO outbox_events (
			id, event_id, topic, key, payload, headers, locked_by, locked_until
		) VALUES (
			$1, $2, 'agenttrace.run.commands.v1', 'run_123',
			'{"event_id":"`+eventID+`","payload":{"run_id":"run_123"}}'::jsonb,
			'{}'::jsonb, 'relay-old', $3
		)`, id, eventID, lockedUntil); err != nil {
		t.Fatalf("insert outbox event: %v", err)
	}
	if lockedUntil == nil {
		if _, err := pool.Exec(ctx, `UPDATE outbox_events SET locked_by = NULL WHERE id = $1`, id); err != nil {
			t.Fatalf("clear initial lease: %v", err)
		}
	}
}
