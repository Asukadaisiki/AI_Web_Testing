package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKafkaComposeDeliversRunMessagesInOrderAndDeduplicatesInbox(t *testing.T) {
	const integrationTopic = "agenttrace.test.commands.v1"

	brokers := os.Getenv("AGENTTRACE_TEST_KAFKA_BROKERS")
	dsn := os.Getenv("AGENTTRACE_TEST_POSTGRES_DSN")
	if brokers == "" || dsn == "" {
		t.Skip("AGENTTRACE_TEST_KAFKA_BROKERS and AGENTTRACE_TEST_POSTGRES_DSN are required")
	}
	if _, err := testpostgres.DatabaseName(dsn); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := newIntegrationPool(t, ctx, dsn)
	publisher, err := kafka.New(kafka.Config{Brokers: []string{brokers}})
	if err != nil {
		t.Fatalf("create Kafka publisher: %v", err)
	}
	t.Cleanup(func() { _ = publisher.Close() })
	consumerBus, err := kafka.New(kafka.Config{Brokers: []string{brokers}})
	if err != nil {
		t.Fatalf("create Kafka consumer: %v", err)
	}
	t.Cleanup(func() { _ = consumerBus.Close() })

	runID := fmt.Sprintf("run_kafka_%d", time.Now().UnixNano())
	first := envelope(t, "evt_kafka_first_"+runID, runID, 1)
	second := envelope(t, "evt_kafka_second_"+runID, runID, 2)
	for _, value := range [][]byte{first, second} {
		if _, err := publisher.Publish(ctx, integrationTopic, runID, value); err != nil {
			t.Fatalf("publish run message: %v", err)
		}
	}

	var (
		mu       sync.Mutex
		received []eventbus.Message
	)
	receivedTwo := make(chan struct{})
	consumeCtx, stopConsume := context.WithCancel(ctx)
	defer stopConsume()
	done := make(chan error, 1)
	go func() {
		done <- consumerBus.Consume(consumeCtx, integrationTopic, "agenttrace-task4-"+runID, func(_ context.Context, message eventbus.Message) error {
			var parsed struct {
				EventID string `json:"event_id"`
			}
			if err := json.Unmarshal(message.Value, &parsed); err != nil {
				return err
			}
			if parsed.EventID != "evt_kafka_first_"+runID && parsed.EventID != "evt_kafka_second_"+runID {
				return nil
			}
			mu.Lock()
			received = append(received, message)
			count := len(received)
			mu.Unlock()
			if count == 2 {
				close(receivedTwo)
			}
			return nil
		})
	}()

	select {
	case <-receivedTwo:
	case <-ctx.Done():
		t.Fatalf("timed out consuming run messages")
	}
	stopConsume()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("consume run messages: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("received = %d, want 2", len(received))
	}
	if string(received[0].Value) != string(first) || string(received[1].Value) != string(second) {
		t.Fatalf("same-run records were not delivered in publish order")
	}

	deduplicator := consumer.NewDeduplicator(pool, "kafka-integration")
	effects := 0
	handler := func(ctx context.Context, tx consumer.Tx, _ eventbus.Message) error {
		effects++
		_, err := tx.Exec(ctx, `
			INSERT INTO projects (id, name) VALUES ($1, 'Kafka effect')
			ON CONFLICT (id) DO NOTHING`, "prj_"+runID)
		return err
	}
	if err := deduplicator.Handle(context.Background(), received[0], handler); err != nil {
		t.Fatalf("first deduplicated handle: %v", err)
	}
	if err := deduplicator.Handle(context.Background(), received[0], handler); err != nil {
		t.Fatalf("duplicate deduplicated handle: %v", err)
	}
	if effects != 1 {
		t.Fatalf("durable handler calls = %d, want 1", effects)
	}
	var inboxRows, persistedEffects int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_events
		WHERE consumer = 'kafka-integration' AND event_id = $1`, "evt_kafka_first_"+runID).Scan(&inboxRows); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM projects WHERE id = $1`, "prj_"+runID).Scan(&persistedEffects); err != nil {
		t.Fatalf("count persisted effects: %v", err)
	}
	if inboxRows != 1 || persistedEffects != 1 {
		t.Fatalf("inbox rows=%d persisted effects=%d, want one each", inboxRows, persistedEffects)
	}
}

func envelope(t *testing.T, eventID, runID string, sequence int) []byte {
	t.Helper()

	value, err := json.Marshal(map[string]any{
		"schema_version": "agenttrace.event.v1",
		"event_id":       eventID,
		"event_type":     "run.requested",
		"aggregate_id":   runID,
		"occurred_at":    "2026-10-21T12:00:00Z",
		"correlation_id": runID,
		"payload": map[string]any{
			"run_id":   runID,
			"sequence": sequence,
		},
	})
	if err != nil {
		t.Fatalf("marshal event envelope: %v", err)
	}
	return value
}

func newIntegrationPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()

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
	return pool
}
