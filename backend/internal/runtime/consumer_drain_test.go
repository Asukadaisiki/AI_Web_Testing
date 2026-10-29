package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

func TestConsumerDrainRelinquishesDeferredDeliveryWithoutRetries(t *testing.T) {
	var drained atomic.Bool
	var calls atomic.Int32
	retries := &memoryRetryStore{}
	c := Consumer{
		Ready: func(context.Context) (bool, error) { return !drained.Load(), nil },
		Inbox: &memoryInbox{}, RetryStore: retries, MaxAttempts: 1,
		Runner: inboxRunnerFunc(func(context.Context, event.RunCommand) error {
			calls.Add(1)
			drained.Store(true)
			return inboxconsumer.DeferDelivery(errors.New("capacity busy"))
		}),
	}
	bus := &singleDeliveryBus{Fake: &eventbus.Fake{}, message: commandMessage(t, "drain-deferred", commandFor("drain-deferred"))}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Consume(ctx, bus, "runtime"); !errors.Is(err, ErrConsumerPaused) {
		t.Fatalf("drained consumer retained delivery: %v", err)
	}
	if calls.Load() != 1 || len(retries.incremented) != 0 || retries.handoffCount != 0 {
		t.Fatalf("drain ran/spent retries: calls=%d retries=%#v", calls.Load(), retries)
	}
}

func TestConsumerDrainDoesNotCancelActiveHandler(t *testing.T) {
	var drained atomic.Bool
	started, finish := make(chan struct{}), make(chan struct{})
	c := Consumer{
		Ready: func(context.Context) (bool, error) { return !drained.Load(), nil },
		Inbox: inboxHandleFunc(func(ctx context.Context, _ eventbus.Message, handler func(context.Context) error) error {
			return handler(ctx)
		}),
		Runner: inboxRunnerFunc(func(ctx context.Context, _ event.RunCommand) error {
			close(started)
			select {
			case <-finish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	}
	bus := &singleDeliveryBus{Fake: &eventbus.Fake{}, message: commandMessage(t, "drain-active", commandFor("drain-active"))}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Consume(ctx, bus, "runtime") }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("active handler did not start")
	}
	drained.Store(true)
	select {
	case err := <-done:
		t.Fatalf("drain cancelled the active delivery before release: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatalf("drain interrupted active delivery: %v", err)
	}
}

func TestConsumerReadinessFailureDoesNotClaimOrDeadLetter(t *testing.T) {
	for _, probeError := range []error{nil, errors.New("database unreachable")} {
		retries := &memoryRetryStore{}
		inbox := &memoryInbox{}
		runner := &recordingRunner{}
		c := Consumer{Ready: func(context.Context) (bool, error) { return false, probeError },
			Inbox: inbox, Runner: runner, RetryStore: retries, MaxAttempts: 1}
		bus := &singleDeliveryBus{Fake: &eventbus.Fake{}, message: commandMessage(t, "drain-unready", commandFor("drain-unready"))}
		if err := c.Consume(context.Background(), bus, "runtime"); !errors.Is(err, ErrConsumerPaused) {
			t.Fatalf("unready consumer: %v", err)
		}
		if runner.calls != 0 || len(inbox.seen) != 0 || len(retries.incremented) != 0 || retries.handoffCount != 0 {
			t.Fatalf("unready consumer changed delivery: runner=%d inbox=%#v retries=%#v", runner.calls, inbox, retries)
		}
	}
}
