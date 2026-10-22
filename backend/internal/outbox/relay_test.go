package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
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

func TestRelayDoesNotAcknowledgeReclaimedLeaseWithSameWorkerID(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_stale_ack", "evt_stale_ack", nil)

	firstBus := newBlockingBus(nil)
	firstRelay := outbox.NewRelay(pool, firstBus, outbox.Config{
		WorkerID:      "shared-worker",
		BatchSize:     1,
		LeaseDuration: time.Minute,
	})
	firstResult := relayOnceAsync(firstRelay, ctx)
	firstBus.waitForPublish(t)

	expireLease(t, ctx, pool, "obx_stale_ack")

	secondBus := newBlockingBus(nil)
	secondRelay := outbox.NewRelay(pool, secondBus, outbox.Config{
		WorkerID:      "shared-worker",
		BatchSize:     1,
		LeaseDuration: time.Minute,
	})
	secondResult := relayOnceAsync(secondRelay, ctx)
	secondBus.waitForPublish(t)

	firstBus.releasePublish()
	result := waitForRelay(t, firstResult)
	if result.published != 0 || result.err != nil {
		t.Fatalf("stale relay result = (published=%d, err=%v), want (0, nil)", result.published, result.err)
	}

	assertLeaseIsStillHeld(t, ctx, pool, "obx_stale_ack", "shared-worker")

	secondBus.releasePublish()
	if result := waitForRelay(t, secondResult); result.published != 1 || result.err != nil {
		t.Fatalf("reclaiming relay result = (published=%d, err=%v), want (1, nil)", result.published, result.err)
	}
}

func TestRelayDoesNotFailReclaimedLeaseWithSameWorkerID(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_stale_failure", "evt_stale_failure", nil)

	firstBus := newBlockingBus(errors.New("first relay failed"))
	firstRelay := outbox.NewRelay(pool, firstBus, outbox.Config{
		WorkerID:      "shared-worker",
		BatchSize:     1,
		LeaseDuration: time.Minute,
	})
	firstResult := relayOnceAsync(firstRelay, ctx)
	firstBus.waitForPublish(t)

	expireLease(t, ctx, pool, "obx_stale_failure")

	secondBus := newBlockingBus(nil)
	secondRelay := outbox.NewRelay(pool, secondBus, outbox.Config{
		WorkerID:      "shared-worker",
		BatchSize:     1,
		LeaseDuration: time.Minute,
	})
	secondResult := relayOnceAsync(secondRelay, ctx)
	secondBus.waitForPublish(t)

	firstBus.releasePublish()
	if result := waitForRelay(t, firstResult); result.err == nil {
		t.Fatal("stale relay publish failure was not returned")
	}

	assertLeaseIsStillHeld(t, ctx, pool, "obx_stale_failure", "shared-worker")

	secondBus.releasePublish()
	if result := waitForRelay(t, secondResult); result.published != 1 || result.err != nil {
		t.Fatalf("reclaiming relay result = (published=%d, err=%v), want (1, nil)", result.published, result.err)
	}
}

func TestRelayInstancesPublishSameRunInOutboxOrder(t *testing.T) {
	ctx, pool := newTestPool(t)
	insertOutboxEvent(t, ctx, pool, "obx_order_first", "evt_order_first", nil)
	insertOutboxEvent(t, ctx, pool, "obx_order_second", "evt_order_second", nil)
	if _, err := pool.Exec(ctx, `
		UPDATE outbox_events
		SET created_at = CASE id
			WHEN 'obx_order_first' THEN now() - interval '1 minute'
			WHEN 'obx_order_second' THEN now()
		END
		WHERE id IN ('obx_order_first', 'obx_order_second')`); err != nil {
		t.Fatalf("order outbox events: %v", err)
	}

	bus := newBlockingBus(nil)
	firstRelay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:      "relay-first",
		BatchSize:     10,
		LeaseDuration: time.Minute,
	})
	firstResult := relayOnceAsync(firstRelay, ctx)
	bus.waitForPublish(t)

	secondRelay := outbox.NewRelay(pool, bus, outbox.Config{
		WorkerID:      "relay-second",
		BatchSize:     10,
		LeaseDuration: time.Minute,
	})
	secondResult := relayOnceAsync(secondRelay, ctx)
	select {
	case <-bus.entered:
		bus.releasePublish()
		waitForRelay(t, firstResult)
		waitForRelay(t, secondResult)
		t.Fatal("second relay published a later same-run event before the first event completed")
	case result := <-secondResult:
		if result.published != 0 || result.err != nil {
			t.Fatalf("second relay result while first is leased = (published=%d, err=%v), want (0, nil)", result.published, result.err)
		}
	case <-time.After(time.Second):
		bus.releasePublish()
		waitForRelay(t, firstResult)
		t.Fatal("second relay did not finish while the earlier same-run event was leased")
	}

	bus.releasePublish()
	if result := waitForRelay(t, firstResult); result.published != 1 || result.err != nil {
		t.Fatalf("first relay result = (published=%d, err=%v), want (1, nil)", result.published, result.err)
	}
	if published, err := secondRelay.RelayOnce(ctx); err != nil || published != 1 {
		t.Fatalf("second relay after first acknowledgement = (published=%d, err=%v), want (1, nil)", published, err)
	}

	messages := bus.Published()
	if len(messages) != 2 || string(messages[0].Value) == string(messages[1].Value) {
		t.Fatalf("published messages = %#v, want two distinct events", messages)
	}
	if eventIDFromMessage(t, messages[0]) != "evt_order_first" ||
		eventIDFromMessage(t, messages[1]) != "evt_order_second" {
		t.Fatalf("same-run publish order = %q then %q, want first then second", messages[0].Value, messages[1].Value)
	}
}

type relayResult struct {
	published int
	err       error
}

func relayOnceAsync(relay *outbox.Relay, ctx context.Context) <-chan relayResult {
	results := make(chan relayResult, 1)
	go func() {
		published, err := relay.RelayOnce(ctx)
		results <- relayResult{published: published, err: err}
	}()
	return results
}

type blockingBus struct {
	mu       sync.Mutex
	entered  chan struct{}
	release  chan struct{}
	messages []eventbus.Message
	err      error
}

func newBlockingBus(err error) *blockingBus {
	return &blockingBus{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
		err:     err,
	}
}

func (b *blockingBus) Publish(ctx context.Context, topic, key string, value []byte) (eventbus.Metadata, error) {
	b.mu.Lock()
	b.messages = append(b.messages, eventbus.Message{
		Topic: topic,
		Key:   key,
		Value: append([]byte(nil), value...),
	})
	b.mu.Unlock()

	b.entered <- struct{}{}
	select {
	case <-b.release:
		return eventbus.Metadata{Partition: 1, Offset: 1}, b.err
	case <-ctx.Done():
		return eventbus.Metadata{}, ctx.Err()
	}
}

func (b *blockingBus) Consume(context.Context, string, string, eventbus.Handler) error {
	return nil
}

func (b *blockingBus) Close() error {
	return nil
}

func (b *blockingBus) waitForPublish(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("relay did not reach broker publish")
	}
}

func (b *blockingBus) releasePublish() {
	select {
	case <-b.release:
	default:
		close(b.release)
	}
}

func (b *blockingBus) Published() []eventbus.Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	messages := make([]eventbus.Message, len(b.messages))
	copy(messages, b.messages)
	return messages
}

func waitForRelay(t *testing.T, results <-chan relayResult) relayResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("relay did not finish")
		return relayResult{}
	}
}

func expireLease(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET locked_until = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatalf("expire outbox lease: %v", err)
	}
}

func assertLeaseIsStillHeld(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, workerID string) {
	t.Helper()

	var (
		publishedAt *time.Time
		lockedBy    *string
		lockedUntil *time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT published_at, locked_by, locked_until
		FROM outbox_events
		WHERE id = $1`, id,
	).Scan(&publishedAt, &lockedBy, &lockedUntil); err != nil {
		t.Fatalf("read outbox lease: %v", err)
	}
	if publishedAt != nil || lockedBy == nil || *lockedBy != workerID || lockedUntil == nil || !lockedUntil.After(time.Now()) {
		t.Fatalf("stale relay changed current lease: published=%v locked_by=%v locked_until=%v", publishedAt, lockedBy, lockedUntil)
	}
}

func eventIDFromMessage(t *testing.T, message eventbus.Message) string {
	t.Helper()

	var payload struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(message.Value, &payload); err != nil {
		t.Fatalf("decode published outbox message: %v", err)
	}
	return payload.EventID
}

func newTestPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_TEST_POSTGRES_DSN is not configured")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
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
