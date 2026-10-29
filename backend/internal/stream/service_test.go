package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
)

func TestReplayStartsWithDurableEvents(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`)},
		{Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`)},
	}}
	service := NewService(reader, live.NewMemoryStore(), Config{ReadBlock: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestReplayFallsBackWhenRedisIsUnavailable(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`)},
	}}
	service := NewService(reader, unavailableLiveStore{}, Config{
		ReadBlock:    10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	reader.Append(DurableEvent{
		Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"fallback"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "span.completed", Data: json.RawMessage(`{"span":"fallback"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestFallbackResumesLiveReadsWhenRedisRecovers(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`)},
	}}
	store := newRecoveringLiveStore()
	store.onRecovery = func() {
		reader.Append(DurableEvent{
			Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"recovered"}`),
		})
	}
	if err := store.Append(context.Background(), "run-1", live.LiveEvent{
		Type: "run.events.available", Data: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("append recovered notice: %v", err)
	}
	service := NewService(reader, store, Config{
		ReadBlock:    10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "span.completed", Data: json.RawMessage(`{"span":"recovered"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestMissingRedisStreamDoesNotMeanMissingRun(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 7, Type: "run.finished", Data: json.RawMessage(`{"state":"succeeded"}`)},
	}}
	service := NewService(reader, live.NewMemoryStore(), Config{ReadBlock: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-after-flush", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "7", Type: "run.finished", Data: json.RawMessage(`{"state":"succeeded"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestLiveEventsAfterDurableSequenceAreNotDuplicated(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`)},
	}}
	liveStore := live.NewMemoryStore()
	if err := liveStore.Append(context.Background(), "run-1", live.LiveEvent{
		Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	}); err != nil {
		t.Fatalf("append duplicate durable notice: %v", err)
	}
	if err := liveStore.Append(context.Background(), "run-1", live.LiveEvent{
		Type: "token.delta", Data: json.RawMessage(`{"delta":"untrusted"}`),
	}); err != nil {
		t.Fatalf("append token delta: %v", err)
	}
	service := NewService(reader, liveStore, Config{ReadBlock: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	select {
	case event := <-events:
		t.Fatalf("Redis payload or duplicate durable event escaped: %#v", event)
	case <-time.After(30 * time.Millisecond):
	}
	assertNoStreamError(t, streamErrs)
}

func TestSequenceZeroNotificationWakesDurableRead(t *testing.T) {
	reader := &memoryDurableReader{}
	store := &noticeLiveStore{
		events: []live.LiveEvent{{ID: "1-0", Type: "run.events.available", Data: json.RawMessage(`{}`)}},
		onFirstRead: func() {
			reader.Append(DurableEvent{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"status":"running"}`)})
		},
	}
	service := NewService(reader, store, Config{ReadBlock: time.Minute, PollInterval: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "")
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"status":"running"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestDurablePollingContinuesWithoutRedisNotifications(t *testing.T) {
	reader := &memoryDurableReader{}
	store := &noticeLiveStore{
		onFirstRead: func() {
			reader.Append(DurableEvent{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{}`)})
		},
	}
	service := NewService(reader, store, Config{
		ReadBlock: time.Minute, PollInterval: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "")
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{}`),
	})
	reader.Append(DurableEvent{Sequence: 2, Type: "run.finished", Data: json.RawMessage(`{}`)})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "run.finished", Data: json.RawMessage(`{}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating(t *testing.T) {
	reader := &memoryDurableReader{events: []DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"state":"running"}`)},
	}}
	liveStore := &noticeLiveStore{
		events: []live.LiveEvent{
			{ID: "1-0", Sequence: 3, Type: "run.finished", Data: json.RawMessage(`{"state":"succeeded"}`)},
			{ID: "2-0", Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`)},
		},
		onFirstRead: func() {
			reader.Append(DurableEvent{
				Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
			})
			reader.Append(DurableEvent{
				Sequence: 3, Type: "run.finished", Data: json.RawMessage(`{"state":"succeeded"}`),
			})
		},
	}
	service := NewService(reader, liveStore, Config{ReadBlock: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "1", Type: "run.started", Data: json.RawMessage(`{"state":"running"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "2", Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
	})
	assertStreamEvent(t, receiveStreamEvent(t, events), StreamEvent{
		ID: "3", Type: "run.finished", Data: json.RawMessage(`{"state":"succeeded"}`),
	})
	assertNoStreamError(t, streamErrs)
}

func TestSubscribeStopsBlockingReadWhenContextIsCanceled(t *testing.T) {
	service := NewService(&memoryDurableReader{}, live.NewMemoryStore(), Config{ReadBlock: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	events, streamErrs := service.Subscribe(ctx, "run-1", "0")

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("stream emitted an event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after cancellation")
	}
	select {
	case err, ok := <-streamErrs:
		if ok && err != nil {
			t.Fatalf("stream error after cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not close errors after cancellation")
	}
}

type memoryDurableReader struct {
	mu     sync.Mutex
	events []DurableEvent
}

func (r *memoryDurableReader) ReadAfter(_ context.Context, _ string, afterSequence int64) ([]DurableEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []DurableEvent
	for _, event := range r.events {
		if event.Sequence > afterSequence {
			result = append(result, event)
		}
	}
	return result, nil
}

func (r *memoryDurableReader) Append(event DurableEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

type unavailableLiveStore struct{}

func (unavailableLiveStore) Append(context.Context, string, live.LiveEvent) error {
	return errors.New("redis is unavailable")
}

func (unavailableLiveStore) Read(context.Context, string, string, time.Duration) ([]live.LiveEvent, error) {
	return nil, errors.New("redis is unavailable")
}

type recoveringLiveStore struct {
	*live.MemoryStore
	failed     bool
	onRecovery func()
}

func newRecoveringLiveStore() *recoveringLiveStore {
	return &recoveringLiveStore{MemoryStore: live.NewMemoryStore()}
}

func (s *recoveringLiveStore) Read(
	ctx context.Context, runID, afterID string, block time.Duration,
) ([]live.LiveEvent, error) {
	if !s.failed {
		s.failed = true
		return nil, errors.New("redis is unavailable")
	}
	if s.onRecovery != nil {
		s.onRecovery()
		s.onRecovery = nil
	}
	return s.MemoryStore.Read(ctx, runID, afterID, block)
}

type noticeLiveStore struct {
	mu          sync.Mutex
	events      []live.LiveEvent
	onFirstRead func()
	read        bool
}

func (s *noticeLiveStore) Append(context.Context, string, live.LiveEvent) error {
	return errors.New("append is not supported")
}

func (s *noticeLiveStore) Read(
	ctx context.Context, _ string, _ string, block time.Duration,
) ([]live.LiveEvent, error) {
	s.mu.Lock()
	if !s.read {
		s.read = true
		events := s.events
		onFirstRead := s.onFirstRead
		s.mu.Unlock()
		onFirstRead()
		return events, nil
	}
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(block):
		return nil, nil
	}
}

func receiveStreamEvent(t *testing.T, events <-chan StreamEvent) StreamEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("stream closed before expected event")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream event")
		return StreamEvent{}
	}
}

func assertStreamEvent(t *testing.T, got, want StreamEvent) {
	t.Helper()
	var gotData, wantData bytes.Buffer
	if err := json.Compact(&gotData, got.Data); err != nil {
		t.Fatalf("compact received stream event JSON: %v", err)
	}
	if err := json.Compact(&wantData, want.Data); err != nil {
		t.Fatalf("compact expected stream event JSON: %v", err)
	}
	if got.ID != want.ID || got.Type != want.Type || gotData.String() != wantData.String() {
		t.Fatalf("stream event = %#v, want %#v", got, want)
	}
}

func assertNoStreamError(t *testing.T, streamErrs <-chan error) {
	t.Helper()
	select {
	case err := <-streamErrs:
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	case <-time.After(25 * time.Millisecond):
	}
}
