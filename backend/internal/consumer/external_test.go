package consumer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

func TestDeferredDeliveryPreservesCauseAndClassificationThroughWrapping(t *testing.T) {
	cause := errors.New("capacity busy")
	err := fmt.Errorf("runner: %w", consumer.DeferDelivery(cause))
	if !errors.Is(err, cause) || !consumer.IsDeliveryDeferred(err) {
		t.Fatalf("wrapped contention lost cause or deferred classification: %v", err)
	}
	if consumer.DeferDelivery(nil) != nil || consumer.IsDeliveryDeferred(nil) || consumer.IsDeliveryDeferred(cause) {
		t.Fatal("nil or ordinary failures classified as deferred")
	}
}

func TestExternalInboxPanicStopsRenewalAndAllowsAbandonedRedelivery(t *testing.T) {
	ctx, pool := newConsumerTestPool(t)
	d := consumer.NewDeduplicator(pool, "runtime")
	d.ClaimTTL = 120 * time.Millisecond
	message := eventbus.Message{Value: []byte(`{"schema_version":"agenttrace.event.v1","event_id":"evt-panicked-handler"}`)}
	cause := errors.New("handler panic")
	func() {
		defer func() {
			if got := recover(); got != cause {
				t.Errorf("handler panic = %v, want original panic", got)
			}
		}()
		_ = d.HandleExternal(ctx, message, func(_ context.Context) error { panic(cause) })
	}()
	time.Sleep(360 * time.Millisecond)
	calls := 0
	if err := d.HandleExternal(ctx, message, func(_ context.Context) error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("redelivery of panicked handler: %v", err)
	}
	if calls != 1 {
		t.Fatalf("recovery callbacks = %d, want 1", calls)
	}
}
