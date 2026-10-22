package redis

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/testredis"
	goredis "github.com/redis/go-redis/v9"
)

func TestClientStoresCappedExpiringLiveEventsAndHints(t *testing.T) {
	raw := integrationRedisClient(t)
	ctx := context.Background()
	client := NewClient(raw)

	if err := client.Append(ctx, "run-1", live.LiveEvent{
		Sequence: 9, Type: "span.completed", Data: json.RawMessage(`{"span":"plan"}`),
	}); err != nil {
		t.Fatalf("append live event: %v", err)
	}
	events, err := client.Read(ctx, "run-1", "0-0", 0)
	if err != nil {
		t.Fatalf("read live events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("live event count = %d, want 1", len(events))
	}
	if events[0].ID == "" || events[0].Sequence != 9 || events[0].Type != "span.completed" ||
		string(events[0].Data) != `{"span":"plan"}` {
		t.Fatalf("live event = %#v", events[0])
	}
	assertTTL(t, raw, liveKey("run-1"), live.StreamTTL)

	for range live.MaxStreamLength + 200 {
		if err := client.Append(ctx, "run-1", live.LiveEvent{Type: "token.delta", Data: json.RawMessage(`{}`)}); err != nil {
			t.Fatalf("append capped event: %v", err)
		}
	}
	length, err := raw.XLen(ctx, liveKey("run-1")).Result()
	if err != nil {
		t.Fatalf("read stream length: %v", err)
	}
	if length > live.MaxStreamLength+200 {
		t.Fatalf("stream length = %d, MAXLEN approximation did not trim", length)
	}

	if err := client.SetLease(ctx, "run-1", "worker-1", live.LeaseTTL); err != nil {
		t.Fatalf("set lease: %v", err)
	}
	lease, err := raw.Get(ctx, leaseKey("run-1")).Result()
	if err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if lease != "worker-1" {
		t.Fatalf("lease owner = %q, want worker-1", lease)
	}
	assertTTL(t, raw, leaseKey("run-1"), live.LeaseTTL)

	if err := client.RequestCancel(ctx, "run-1", live.CancelTTL); err != nil {
		t.Fatalf("request cancel: %v", err)
	}
	canceled, err := client.IsCancelRequested(ctx, "run-1")
	if err != nil {
		t.Fatalf("read cancel hint: %v", err)
	}
	if !canceled {
		t.Fatal("cancel hint was not recorded")
	}
	assertTTL(t, raw, cancelKey("run-1"), live.CancelTTL)
}

func TestClientTreatsFlushedStreamAsEmptyAndCancelsBlockingRead(t *testing.T) {
	raw := integrationRedisClient(t)
	ctx := context.Background()
	client := NewClient(raw)
	if err := client.Append(ctx, "run-1", live.LiveEvent{
		Sequence: 1, Type: "run.started", Data: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("append live event: %v", err)
	}
	if err := raw.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	events, err := client.Read(ctx, "run-1", "0-0", 0)
	if err != nil {
		t.Fatalf("read flushed stream: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("flushed stream events = %#v, want empty", events)
	}

	readCtx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Read(readCtx, "run-1", "$", time.Minute)
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("blocking read returned nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("blocking read did not honor context cancellation")
	}
}

func integrationRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	address := os.Getenv("AGENTTRACE_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("AGENTTRACE_TEST_REDIS_ADDR is not configured")
	}
	target, err := testredis.Target(address, os.Getenv("AGENTTRACE_TEST_REDIS_DB"))
	if err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(&goredis.Options{Addr: target.Address, DB: target.Database})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("reset redis: %v", err)
	}
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background()).Err()
		_ = client.Close()
	})
	return client
}

func assertTTL(t *testing.T, client *goredis.Client, key string, want time.Duration) {
	t.Helper()
	got, err := client.TTL(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("read TTL for %s: %v", key, err)
	}
	if got <= 0 || got > want {
		t.Fatalf("TTL for %s = %s, want in (0, %s]", key, got, want)
	}
}
