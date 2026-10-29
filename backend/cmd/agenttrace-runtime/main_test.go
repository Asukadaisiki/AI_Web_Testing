package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime"
)

func TestRuntimeWiresPiRunnerAsSoleCommandRunner(t *testing.T) {
	cfg := config.Config{
		RuntimeConcurrency: 2,
		Harness: config.Harness{
			Command:          "node",
			Entry:            "/app/pi-browser-harness/dist/main.js",
			Directory:        "/app",
			Timeout:          20 * time.Minute,
			TerminationGrace: 5 * time.Second,
			MaxLineBytes:     4194304,
			MaxStderrBytes:   65536,
		},
	}
	runner := newCommandRunner(cfg, nil)
	bounded, ok := runner.(*boundedRunner)
	if !ok {
		t.Fatalf("command runner = %T, want *boundedRunner", runner)
	}
	piRunner, ok := bounded.runner.(runtime.PiRunner)
	if !ok {
		t.Fatalf("bounded runner inner = %T, want runtime.PiRunner", bounded.runner)
	}
	profile, err := agentprofile.LoadBrowserEval()
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	if piRunner.DefaultReasoningEffort != profile.Model.ReasoningEffort {
		t.Fatalf(
			"PiRunner default reasoning effort = %q, want profile value %q",
			piRunner.DefaultReasoningEffort, profile.Model.ReasoningEffort,
		)
	}
	closer, ok := runner.(interface{ Close() error })
	if !ok {
		t.Fatal("runtime runner does not own Harness shutdown")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close unused harness: %v", err)
	}
}

func TestRuntimePoolConfigReservesConnectionsForConcurrentInboxHandlers(t *testing.T) {
	poolConfig, err := runtimePoolConfig(
		"postgres://agenttrace:agenttrace@postgres:5432/agenttrace?sslmode=disable",
		8,
	)
	if err != nil {
		t.Fatalf("create runtime pool config: %v", err)
	}
	if poolConfig.MaxConns != 18 {
		t.Fatalf("max connections = %d, want 18", poolConfig.MaxConns)
	}

	explicit, err := runtimePoolConfig(
		"postgres://agenttrace:agenttrace@postgres:5432/agenttrace?sslmode=disable&pool_max_conns=24",
		8,
	)
	if err != nil {
		t.Fatalf("create explicitly sized runtime pool config: %v", err)
	}
	if explicit.MaxConns != 24 {
		t.Fatalf("explicit max connections = %d, want 24", explicit.MaxConns)
	}
}

func TestHarnessEnvironmentUsesNodeHarnessModelVariableNames(t *testing.T) {
	t.Setenv("AGENTTRACE_MODEL_BASE_URL", "https://inherited.example/v1")
	t.Setenv("AGENTTRACE_MODEL_API_KEY", "inherited-key")
	t.Setenv("PI_HARNESS_MODEL_BASE_URL", "")
	t.Setenv("PI_HARNESS_MODEL_API_KEY", "")

	env := harnessEnvironment(config.Harness{
		ProviderBaseURL: "https://configured.example/v1",
		ProviderAPIKey:  "configured-key",
	})

	if got := lastEnvironmentValue(env, "AGENTTRACE_MODEL_BASE_URL"); got != "https://configured.example/v1" {
		t.Fatalf("model base URL = %q, want configured value", got)
	}
	if got := lastEnvironmentValue(env, "AGENTTRACE_MODEL_API_KEY"); got != "configured-key" {
		t.Fatalf("model API key = %q, want configured value", got)
	}
	if got := lastEnvironmentValue(env, "PI_HARNESS_MODEL_BASE_URL"); got != "" {
		t.Fatalf("legacy model base URL unexpectedly set to %q", got)
	}
	if got := lastEnvironmentValue(env, "PI_HARNESS_MODEL_API_KEY"); got != "" {
		t.Fatalf("legacy model API key unexpectedly set to %q", got)
	}
}

func lastEnvironmentValue(env []string, name string) string {
	prefix := name + "="
	var value string
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
		}
	}
	return value
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
