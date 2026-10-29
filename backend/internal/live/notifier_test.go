package live

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

func TestNotifierWritesOnlyWakeupsForRunEvents(t *testing.T) {
	store := NewMemoryStore()
	notifier := Notifier{Store: store}
	message := notificationMessage()
	for range 2 {
		if err := notifier.Handle(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	notices, err := store.Read(context.Background(), "run-notify", "0-0", 0)
	if err != nil || len(notices) != 2 {
		t.Fatalf("notices = %#v, err = %v", notices, err)
	}
	for _, notice := range notices {
		if notice.Type != "run.events.available" || notice.Sequence != 0 || string(notice.Data) != "{}" {
			t.Fatalf("notice copied authoritative event data: %#v", notice)
		}
	}
}

func TestNotifierDiscardsInvalidRecordsWithoutBlockingProgress(t *testing.T) {
	for _, raw := range []string{
		`not json`,
		`{"schema_version":"agenttrace.event.v2","event_id":"e","aggregate_id":"r","event_type":"run.started"}`,
		`{"schema_version":"agenttrace.run-command.v1","event_id":"e","aggregate_id":"r","event_type":"run.started"}`,
		`{"schema_version":"agenttrace.event.v1","event_id":"e","aggregate_id":" ","event_type":"run.started"}`,
		`{"schema_version":"agenttrace.event.v1","aggregate_id":"r","event_type":"run.started"}`,
		`{"schema_version":"agenttrace.event.v1","event_id":"e","aggregate_id":"r"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			store := NewMemoryStore()
			var reported error
			notifier := Notifier{Store: store, OnError: func(err error) { reported = err }}
			if err := notifier.Handle(context.Background(), eventbus.Message{
				Topic: "agenttrace.run.events.v1", Value: []byte(raw),
			}); err != nil {
				t.Fatalf("invalid disposable notification blocked progress: %v", err)
			}
			if reported == nil {
				t.Fatal("invalid notification was not reported")
			}
			notices, err := store.Read(context.Background(), "r", "0-0", 0)
			if err != nil || len(notices) != 0 {
				t.Fatalf("invalid notices = %#v, err = %v", notices, err)
			}
		})
	}
}

func TestNotifierRetriesRedisFailureWithIndependentKafkaProgress(t *testing.T) {
	store := &failingOnceStore{MemoryStore: NewMemoryStore()}
	bus := &notificationBus{message: notificationMessage(), committed: make(chan struct{})}
	notifier := Notifier{Store: store, RetryInterval: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- notifier.Consume(ctx, bus) }()
	select {
	case <-bus.committed:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("failed notification was not retried")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("consume exit = %v", err)
	}
	if bus.attempts != 2 || bus.topic != "agenttrace.run.events.v1" || bus.group != "agenttrace-live-notifier" {
		t.Fatalf("subscription = %#v", bus)
	}
	notices, err := store.Read(context.Background(), "run-notify", "0-0", 0)
	if err != nil || len(notices) != 1 {
		t.Fatalf("retried notices = %#v, err = %v", notices, err)
	}
}

func TestNotifierStopsDuringRetryWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := &eventbus.Fake{ConsumeErr: errors.New("Kafka unavailable")}
	notifier := Notifier{
		Store: NewMemoryStore(), RetryInterval: time.Hour,
		OnError: func(error) { cancel() },
	}
	done := make(chan error, 1)
	go func() { done <- notifier.Consume(ctx, bus) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consume exit = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored cancellation")
	}
}

func notificationMessage() eventbus.Message {
	return eventbus.Message{
		Topic: "agenttrace.run.events.v1", Key: "run-notify",
		Value: json.RawMessage(`{"schema_version":"agenttrace.event.v1","event_id":"evt-notify","aggregate_id":"run-notify","event_type":"harness.tool.completed","occurred_at":"2026-10-28T00:00:00Z","correlation_id":"run-notify","payload":{"secret":"must stay in PostgreSQL","source_sequence":99}}`),
	}
}

type failingOnceStore struct {
	*MemoryStore
	failed bool
}

func (s *failingOnceStore) Append(ctx context.Context, runID string, notice LiveEvent) error {
	if !s.failed {
		s.failed = true
		return errors.New("Redis unavailable")
	}
	return s.MemoryStore.Append(ctx, runID, notice)
}

// notificationBus models EventBus's commit-after-success contract.
type notificationBus struct {
	eventbus.Fake
	mu        sync.Mutex
	message   eventbus.Message
	topic     string
	group     string
	attempts  int
	committed chan struct{}
}

func (b *notificationBus) Consume(ctx context.Context, topic, group string, handle eventbus.Handler) error {
	b.mu.Lock()
	b.topic, b.group = topic, group
	b.attempts++
	b.mu.Unlock()
	if err := handle(ctx, b.message); err != nil {
		return err
	}
	close(b.committed)
	<-ctx.Done()
	return ctx.Err()
}
