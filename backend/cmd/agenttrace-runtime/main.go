// Command agenttrace-runtime is the v3 command-consumer process.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("agenttrace-runtime configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolConfig, err := runtimePoolConfig(cfg.Postgres.DSN, cfg.RuntimeConcurrency)
	if err != nil {
		return fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate PostgreSQL: %w", err)
	}

	bus, err := kafka.New(kafka.Config{Brokers: cfg.Kafka.Brokers})
	if err != nil {
		return fmt.Errorf("open Kafka: %w", err)
	}
	defer bus.Close()

	store := postgres.NewStore(pool)
	runtimeNode, browserNode, err := configuredWorkers(cfg)
	if err != nil {
		return fmt.Errorf("configure workers: %w", err)
	}
	if _, err := store.RegisterWorker(ctx, runtimeNode); err != nil {
		return fmt.Errorf("register runtime worker: %w", err)
	}
	if err := probeBrowserWorker(ctx, browserNode.Endpoint); err != nil {
		return fmt.Errorf("browser worker readiness: %w", err)
	}
	if _, err := store.RegisterWorker(ctx, browserNode); err != nil {
		return fmt.Errorf("register browser worker: %w", err)
	}
	workerID := runtimeNode.ID
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		maintainWorkers(workerCtx, store, runtimeNode.ID, browserNode)
	}()
	defer func() {
		cancelWorkers()
		<-workerDone
	}()
	runner := newCommandRunner(cfg, store, workerID)
	defer func() {
		if err := runner.Close(); err != nil {
			log.Printf("close session host: %v", err)
		}
	}()
	consumerRuntime := &runtime.Consumer{
		Inbox: runtime.DeduplicatedInbox{
			Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-runtime"),
		},
		Runner:     runner,
		Publisher:  bus,
		RetryStore: runtime.PostgresRetryStore{Store: store, Consumer: "agenttrace-runtime"},
		DLQTopic:   cfg.Kafka.DLQTopic,
	}
	consumerRuntime.Ready = func(ctx context.Context) (bool, error) {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return store.WorkerReady(probeCtx, workerID)
	}
	if err := consumeWorkers(ctx, cfg.RuntimeConcurrency, func(workerCtx context.Context) error {
		return consumeWhenReady(workerCtx, consumerRuntime.Ready, func(ctx context.Context) error {
			return consumerRuntime.Consume(ctx, bus, cfg.Kafka.GroupID)
		}, 250*time.Millisecond)
	}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("consume AgentTrace commands: %w", err)
	}
	return nil
}

type managedCommandRunner interface {
	runtime.CommandRunner
	Close() error
}

// newCommandRunner bounds session concurrency and owns one shared host
// transport. Model execution and the selected Harness live behind that
// transport; Langfuse observes persisted events independently.
func newCommandRunner(cfg config.Config, store *postgres.Store, workerID string) managedCommandRunner {
	hostEnvironment := harnessEnvironment(cfg.Harness)
	if cfg.BrowserProviderToken != "" {
		hostEnvironment = append(hostEnvironment, "AGENTTRACE_BROWSER_PROVIDER_TOKEN="+cfg.BrowserProviderToken)
	}
	process := &piharness.SharedProcessRunner{
		Command:          cfg.Harness.Command,
		Args:             []string{cfg.Harness.Entry, "--server"},
		WorkingDirectory: cfg.Harness.Directory,
		Environment:      hostEnvironment,
		MaxLineBytes:     cfg.Harness.MaxLineBytes,
		MaxStderrBytes:   cfg.Harness.MaxStderrBytes,
		TerminationGrace: cfg.Harness.TerminationGrace,
		Timeout:          cfg.Harness.Timeout,
	}
	piRunner := runtime.PiRunner{
		Process:                process,
		Store:                  runtime.PostgresPiTraceStore{Store: store, RuntimeWorkerID: workerID},
		DefaultReasoningEffort: runtimeReasoningEffort(cfg.Model),
	}
	bounded, err := newBoundedRunner(piRunner, cfg.RuntimeConcurrency)
	if err != nil {
		panic(err)
	}
	bounded.closeHarness = process.Close
	return bounded
}

func configuredWorkers(cfg config.Config) (postgres.WorkerRegistration, postgres.WorkerRegistration, error) {
	if strings.TrimSpace(cfg.BrowserProviderToken) == "" {
		return postgres.WorkerRegistration{}, postgres.WorkerRegistration{}, errors.New("management runtime requires AGENTTRACE_BROWSER_PROVIDER_TOKEN")
	}
	capacity := 1
	if raw := os.Getenv("AGENTTRACE_BROWSER_WORKER_CAPACITY"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			return postgres.WorkerRegistration{}, postgres.WorkerRegistration{}, errors.New("AGENTTRACE_BROWSER_WORKER_CAPACITY must be 1..1000")
		}
		capacity = n
	}
	runtimeID := strings.TrimSpace(os.Getenv("AGENTTRACE_RUNTIME_WORKER_ID"))
	if runtimeID == "" {
		host, err := os.Hostname()
		if err != nil {
			return postgres.WorkerRegistration{}, postgres.WorkerRegistration{}, err
		}
		runtimeID = fmt.Sprintf("runtime-%s-%d-%s", host, os.Getpid(), rand.Text())
	}
	browserID := strings.TrimSpace(os.Getenv("AGENTTRACE_BROWSER_WORKER_ID"))
	if browserID == "" {
		browserID = postgres.BrowserWorkerID(cfg.WorkerURL)
		if browserID == "" {
			return postgres.WorkerRegistration{}, postgres.WorkerRegistration{}, errors.New("invalid browser worker endpoint")
		}
	}
	project := strings.TrimSpace(os.Getenv("AGENTTRACE_WORKER_PROJECT_ID"))
	return postgres.WorkerRegistration{ID: runtimeID, Role: "runtime", Capacity: cfg.RuntimeConcurrency, ProjectID: project,
			Version: "agenttrace-runtime.v3", RuntimeIDs: []string{"pi", "claude-code", "codex"}},
		postgres.WorkerRegistration{ID: browserID, Role: "browser", Capacity: capacity, ProjectID: project,
			Endpoint: cfg.WorkerURL, Version: "browser-worker.v3"}, nil
}

func probeBrowserWorker(ctx context.Context, endpoint string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/health", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("browser health returned %d", response.StatusCode)
	}
	return nil
}

func maintainWorkers(ctx context.Context, store *postgres.Store, runtimeID string, browser postgres.WorkerRegistration) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		heartbeatCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		if err := store.HeartbeatWorker(heartbeatCtx, runtimeID); err != nil && ctx.Err() == nil {
			log.Printf("runtime heartbeat failed (claims fail closed): %v", err)
		}
		// Browser liveness is never inferred from runtime liveness.
		if probeBrowserWorker(heartbeatCtx, browser.Endpoint) == nil {
			if err := store.HeartbeatWorker(heartbeatCtx, browser.ID); err != nil && ctx.Err() == nil {
				log.Printf("browser heartbeat failed: %v", err)
			}
		}
		if _, err := store.RecoverExpiredHarnessLeases(heartbeatCtx, 100); err != nil && ctx.Err() == nil {
			log.Printf("lease recovery failed: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runtimeReasoningEffort(model config.Model) string {
	if model.ReasoningEffort != "" {
		return model.ReasoningEffort
	}
	return config.DefaultReasoningEffort
}

func harnessEnvironment(h config.Harness) []string {
	env := os.Environ()
	// Pass model credentials under the names the Node harness reads
	// (AGENTTRACE_MODEL_BASE_URL / AGENTTRACE_MODEL_API_KEY). Explicit
	// values from config override any inherited runtime environment.
	if h.ProviderBaseURL != "" {
		env = append(env, "AGENTTRACE_MODEL_BASE_URL="+h.ProviderBaseURL)
	}
	if h.ProviderAPIKey != "" {
		env = append(env, "AGENTTRACE_MODEL_API_KEY="+h.ProviderAPIKey)
	}
	return env
}

func runtimePoolConfig(dsn string, concurrency int) (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	required := int32(concurrency*2 + 2)
	if poolConfig.MaxConns < required {
		poolConfig.MaxConns = required
	}
	return poolConfig, nil
}

func consumeWorkers(ctx context.Context, count int, consume func(context.Context) error) error {
	if count < 1 {
		return errors.New("runtime consumer count must be at least 1")
	}
	if consume == nil {
		return errors.New("runtime consumer worker is required")
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := consume(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				errs <- err
				cancel()
			}
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return ctx.Err()
}

// Each consumer relinquishes/rejoins independently. Pausing a queued partition
// must not cancel sibling consumers which are still releasing active attempts.
func consumeWhenReady(ctx context.Context, ready func(context.Context) (bool, error), consume func(context.Context) error, interval time.Duration) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		canJoin := true
		if ready != nil {
			var err error
			canJoin, err = ready(ctx)
			canJoin = canJoin && err == nil
		}
		if canJoin {
			err := consume(ctx)
			if !errors.Is(err, runtime.ErrConsumerPaused) {
				return err
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type boundedRunner struct {
	runner       runtime.CommandRunner
	slots        chan struct{}
	closeHarness func() error
}

func newBoundedRunner(runner runtime.CommandRunner, limit int) (*boundedRunner, error) {
	if runner == nil {
		return nil, errors.New("runtime command runner is required")
	}
	if limit < 1 {
		return nil, errors.New("runtime concurrency must be at least 1")
	}
	return &boundedRunner{runner: runner, slots: make(chan struct{}, limit)}, nil
}

func (r *boundedRunner) Run(ctx context.Context, command event.RunCommand) error {
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.slots }()
	return r.runner.Run(ctx, command)
}

func (r *boundedRunner) Close() error {
	if r.closeHarness != nil {
		return r.closeHarness()
	}
	return nil
}
