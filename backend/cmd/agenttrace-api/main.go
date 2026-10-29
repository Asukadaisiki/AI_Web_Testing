// Command agenttrace-api is the v3 HTTP process.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	v3 "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/api/v3"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/browserprovider"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	liveredis "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/serviceprobe"
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
	store := postgres.NewStore(pool)
	stopAPIHeartbeat := store.StartServiceHeartbeat(ctx, "api", processID("api"), "")
	defer stopAPIHeartbeat()
	if err := store.SetEnvironmentDefaultPolicy(cfg.BrowserAllowedDomains); err != nil {
		return fmt.Errorf("configure browser domain policy: %w", err)
	}

	bus, err := kafka.New(kafka.Config{Brokers: cfg.Kafka.Brokers})
	if err != nil {
		return fmt.Errorf("open Kafka: %w", err)
	}
	defer bus.Close()
	var kafkaLag func(context.Context) serviceprobe.Measurement
	probe, probeErr := serviceprobe.NewKafkaLag(cfg.Kafka)
	if probeErr == nil {
		defer probe.Close()
		kafkaLag = probe.Measure
	}
	provider, err := browserprovider.New(cfg.WorkerURL, cfg.BrowserProviderToken)
	if err != nil {
		return fmt.Errorf("configure browser provider: %w", err)
	}
	lifecycleCtx, cancelLifecycle := context.WithCancel(ctx)
	lifecycleDone := make(chan struct{})
	go func() {
		defer close(lifecycleDone)
		_ = browserprovider.ConsumeLifecycle(lifecycleCtx, bus, store, provider, func(err error) {
			log.Printf("environment lifecycle: %v", err)
		})
	}()
	defer func() { cancelLifecycle(); <-lifecycleDone }()

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
		notifier := &live.Notifier{
			Store: liveStore,
			OnError: func(err error) {
				log.Printf("live notifier: %v", err)
			},
		}
		notifierCtx, cancelNotifier := context.WithCancel(ctx)
		notifierDone := make(chan struct{})
		go func() {
			defer close(notifierDone)
			if err := notifier.Consume(notifierCtx, bus); err != nil && notifierCtx.Err() == nil {
				log.Printf("live notifier stopped: %v", err)
			}
		}()
		defer func() {
			cancelNotifier()
			<-notifierDone
		}()
	}
	var redisHealth func(context.Context) error
	if redisClient != nil {
		redisHealth = func(checkCtx context.Context) error {
			return redisClient.Ping(checkCtx).Err()
		}
	}
	relayConfig := outbox.Config{WorkerID: processID("api-outbox")}
	relayConfig.Observe = store.NewServiceObserver("outbox_relay", relayConfig.WorkerID, "")
	relay := outbox.NewRelay(pool, bus, relayConfig)
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

	api := v3.New(v3.ServerConfig{
		Repository:      store,
		Runtimes:        cfg.RuntimeCatalog(),
		Stream:          stream.NewService(stream.NewPostgreSQLReader(pool), liveStore, stream.Config{}),
		ArtifactDir:     cfg.ArtifactDir,
		BridgeToken:     cfg.BridgeToken,
		BridgeProjectID: cfg.BridgeProjectID,
		BrowserProvider: provider,
		Health: v3.HealthDependencies{
			Postgres: pool.Ping,
			Kafka:    bus.Health,
			KafkaLag: kafkaLag,
			Redis:    redisHealth,
			Worker:   workerHealthCheck(cfg.WorkerURL),
			Langfuse: langfuseHealthCheck(os.Getenv("AGENTTRACE_LANGFUSE_BASE_URL")),
		},
	})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
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
	return endpointHealthCheck(workerURL, "/health")
}

func langfuseHealthCheck(baseURL string) func(context.Context) error {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return endpointHealthCheck(baseURL, "/api/public/health")
}

func endpointHealthCheck(baseURL, path string) func(context.Context) error {
	return func(ctx context.Context) error {
		parsed, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil {
			return fmt.Errorf("parse worker URL: %w", err)
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + path
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
