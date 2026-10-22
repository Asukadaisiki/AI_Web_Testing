package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
)

func TestSelectModelUsesCheckpointedScriptedFixtureWhenConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(`{"steps":[{"final":"done"}]}`), 0o600); err != nil {
		t.Fatalf("write scripted fixture: %v", err)
	}
	selected, err := selectModel(config.Model{ScriptPath: path})
	if err != nil {
		t.Fatalf("select model: %v", err)
	}
	if _, ok := selected.(*model.CheckpointedScripted); !ok {
		t.Fatalf("model = %T, want checkpointed scripted", selected)
	}
	if _, ok := selected.(model.CheckpointedModel); !ok {
		t.Fatalf("model = %T, want durable checkpointed model", selected)
	}
}

func TestBoundedRunnerHonorsConcurrencyLimit(t *testing.T) {
	runner := &blockingRunner{started: make(chan struct{}, 2), release: make(chan struct{})}
	bounded, err := newBoundedRunner(runner, 1)
	if err != nil {
		t.Fatalf("create bounded runner: %v", err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- bounded.Run(context.Background(), event.RunCommand{RunID: "run"}) }()
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("first run did not start")
	}
	select {
	case <-runner.started:
		t.Fatal("second run started before concurrency slot was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(runner.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
}

func TestConsumeWorkersRunsAssignedPartitionsConcurrentlyAndJoinsAfterFailure(t *testing.T) {
	ctx := context.Background()
	consumer := &fakePartitionConsumer{
		started: make(chan int, 2),
		stopped: make(chan int, 1),
		fail:    make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- consumeWorkers(ctx, 2, consumer.Consume)
	}()

	partitions := map[int]bool{}
	for range 2 {
		select {
		case partition := <-consumer.started:
			partitions[partition] = true
		case <-time.After(time.Second):
			t.Fatal("configured consumer workers did not start concurrently")
		}
	}
	if len(partitions) != 2 {
		t.Fatalf("active consumer partitions = %#v, want two assignments", partitions)
	}
	close(consumer.fail)
	select {
	case partition := <-consumer.stopped:
		if partition != 1 {
			t.Fatalf("cancelled consumer partition = %d, want 1", partition)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer worker survived sibling failure")
	}
	select {
	case err := <-done:
		if !errors.Is(err, errFakeConsumerFailure) {
			t.Fatalf("consume workers error = %v, want %v", err, errFakeConsumerFailure)
		}
	case <-time.After(time.Second):
		t.Fatal("consume workers did not join after cancellation")
	}
}

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRunner) Run(context.Context, event.RunCommand) error {
	r.started <- struct{}{}
	<-r.release
	return nil
}

var errFakeConsumerFailure = errors.New("fake consumer failure")

type fakePartitionConsumer struct {
	mu      sync.Mutex
	next    int
	started chan int
	stopped chan int
	fail    chan struct{}
}

func (c *fakePartitionConsumer) Consume(ctx context.Context) error {
	c.mu.Lock()
	partition := c.next
	c.next++
	c.mu.Unlock()
	c.started <- partition
	if partition == 0 {
		<-c.fail
		return errFakeConsumerFailure
	}
	<-ctx.Done()
	c.stopped <- partition
	return ctx.Err()
}
