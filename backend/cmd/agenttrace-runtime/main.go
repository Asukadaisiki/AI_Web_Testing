// Command agenttrace-runtime is the v3 command-consumer process.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	liveredis "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
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

	var (
		redisClient *goredis.Client
		liveStore   live.LiveStore
	)
	if cfg.Redis.Enabled {
		redisClient = goredis.NewClient(&goredis.Options{
			Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		})
		defer redisClient.Close()
		liveStore = liveredis.NewClient(redisClient)
	}

	store := postgres.NewStore(pool)
	runner := newCommandRunner(cfg, store)
	consumerRuntime := &runtime.Consumer{
		Inbox: runtime.DeduplicatedInbox{
			Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-runtime"),
		},
		Runner:     runner,
		Publisher:  bus,
		RetryStore: runtime.PostgresRetryStore{Store: store, Consumer: "agenttrace-runtime"},
		Live:       liveStore,
		LeaseOwner: processID("runtime"),
		DLQTopic:   cfg.Kafka.DLQTopic,
	}
	if err := consumeWorkers(ctx, cfg.RuntimeConcurrency, func(workerCtx context.Context) error {
		return consumerRuntime.Consume(workerCtx, bus, cfg.Kafka.GroupID)
	}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("consume AgentTrace commands: %w", err)
	}
	return nil
}

// newCommandRunner builds the sole production CommandRunner: a concurrency-
// bounded PiRunner that spawns the pi-browser-harness subprocess. The model
// and HTTP operation dispatcher are intentionally not constructed here; the
// harness owns both the model turn loop and browser operation dispatch.
func newCommandRunner(cfg config.Config, store *postgres.Store) runtime.CommandRunner {
	profile, err := agentprofile.LoadBrowserEval()
	if err != nil {
		panic(fmt.Errorf("load browser profile for runtime: %w", err))
	}
	process := &piharness.ProcessRunner{
		Command:          cfg.Harness.Command,
		Args:             []string{cfg.Harness.Entry},
		WorkingDirectory: cfg.Harness.Directory,
		Environment:      harnessEnvironment(cfg.Harness),
		MaxLineBytes:     cfg.Harness.MaxLineBytes,
		MaxStderrBytes:   cfg.Harness.MaxStderrBytes,
		TerminationGrace: cfg.Harness.TerminationGrace,
		Timeout:          cfg.Harness.Timeout,
	}
	piRunner := runtime.PiRunner{
		Process:                process,
		Store:                  runtime.PostgresPiTraceStore{Store: store},
		WorkerURL:              cfg.WorkerURL,
		DefaultReasoningEffort: profile.Model.ReasoningEffort,
	}
	bounded, err := newBoundedRunner(piRunner, cfg.RuntimeConcurrency)
	if err != nil {
		panic(err)
	}
	return bounded
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

type boundedRunner struct {
	runner runtime.CommandRunner
	slots  chan struct{}
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

func processID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, os.Getpid())
}
