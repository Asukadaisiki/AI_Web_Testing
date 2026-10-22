// Command agenttrace-api is the v3 HTTP process.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	v3 "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/api/v3"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	liveredis "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
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
		return fmt.Errorf("agenttrace-api configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.Postgres.DSN)
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
	var redisHealth func(context.Context) error
	if redisClient != nil {
		redisHealth = func(checkCtx context.Context) error {
			return redisClient.Ping(checkCtx).Err()
		}
	}
	profile, err := agentprofile.LoadBrowserEval(cfg.WorkerURL)
	if err != nil {
		return fmt.Errorf("load browser profile: %w", err)
	}

	relay := outbox.NewRelay(pool, bus, outbox.Config{WorkerID: processID("api-outbox")})
	relayCtx, cancelRelay := context.WithCancel(ctx)
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		relayLoop(relayCtx, relay)
	}()
	defer func() {
		cancelRelay()
		<-relayDone
	}()

	store := postgres.NewStore(pool)
	api := v3.New(v3.ServerConfig{
		Repository:  store,
		Profile:     profile,
		Model:       configuredModel(cfg.Model, profile),
		Stream:      stream.NewService(stream.NewPostgreSQLReader(pool), liveStore, stream.Config{}),
		Live:        liveStore,
		ArtifactDir: cfg.ArtifactDir,
		Health: v3.HealthDependencies{
			Postgres: pool.Ping,
			Kafka:    bus.Health,
			Redis:    redisHealth,
			Worker:   workerHealthCheck(cfg.WorkerURL),
			// Langfuse runs in an optional, asynchronous Compose profile. The
			// API has no control-plane endpoint for that process, so a nil
			// check reports it as disabled instead of inventing a probe.
			Langfuse: nil,
		},
	})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve API: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown API: %w", err)
		}
		return nil
	}
}

func configuredModel(cfg config.Model, profile agentprofile.Profile) event.ModelRef {
	if cfg.ScriptPath != "" {
		return event.ModelRef{Provider: "scripted", Name: "browser-eval-fixture"}
	}
	return event.ModelRef{Provider: profile.Model.Provider, Name: cfg.Name}
}

func relayLoop(ctx context.Context, relay *outbox.Relay) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := relay.RelayOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("outbox relay: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func workerHealthCheck(workerURL string) func(context.Context) error {
	return func(ctx context.Context) error {
		parsed, err := url.Parse(strings.TrimSpace(workerURL))
		if err != nil {
			return fmt.Errorf("parse worker URL: %w", err)
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/health"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return fmt.Errorf("build worker health request: %w", err)
		}
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("request worker health: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("worker health returned HTTP %d", response.StatusCode)
		}
		return nil
	}
}

func processID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, os.Getpid())
}
