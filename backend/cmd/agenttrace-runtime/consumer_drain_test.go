package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime"
)

func TestPausedConsumerRejoinsWithoutCancellingActiveSibling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var worker, joins atomic.Int32
	active, rejoined := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- consumeWorkers(ctx, 2, func(ctx context.Context) error {
			if worker.Add(1) == 1 {
				return consumeWhenReady(ctx, func(context.Context) (bool, error) { return true, nil },
					func(ctx context.Context) error {
						if joins.Add(1) == 1 {
							return runtime.ErrConsumerPaused
						}
						close(rejoined)
						<-ctx.Done()
						return ctx.Err()
					}, time.Millisecond)
			}
			close(active)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	for _, started := range []chan struct{}{active, rejoined} {
		select {
		case <-started:
		case err := <-done:
			t.Fatalf("pause cancelled active siblings: %v", err)
		case <-ctx.Done():
			t.Fatal("consumers did not start/rejoin")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("consumer supervisor shutdown: %v", err)
	}
}

func TestPausedConsumerDoesNotJoinUntilWorkerIsReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var ready atomic.Bool
	probed := make(chan struct{}, 1)
	var joins atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- consumeWhenReady(ctx, func(context.Context) (bool, error) {
			select {
			case probed <- struct{}{}:
			default:
			}
			return ready.Load(), nil
		}, func(context.Context) error { joins.Add(1); return nil }, time.Millisecond)
	}()
	select {
	case <-probed:
	case <-ctx.Done():
		t.Fatal("worker readiness was not checked")
	}
	if joins.Load() != 0 {
		t.Fatal("drained consumer joined Kafka")
	}
	ready.Store(true)
	if err := <-done; err != nil || joins.Load() != 1 {
		t.Fatalf("enabled consumer did not rejoin: %v joins=%d", err, joins.Load())
	}
}
