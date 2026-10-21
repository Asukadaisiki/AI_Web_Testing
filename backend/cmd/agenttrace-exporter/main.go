// Command agenttrace-exporter mirrors completed runs to optional Langfuse.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus/kafka"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/export/langfuse"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.LoadExporter()
	if err != nil {
		return fmt.Errorf("agenttrace-exporter configuration: %w", err)
	}
	if !cfg.Enabled {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runConfigured(ctx, cfg, startEnabled)
}

func runConfigured(
	ctx context.Context,
	cfg config.Exporter,
	start func(context.Context, config.Exporter) error,
) error {
	if !cfg.Enabled {
		return nil
	}
	if start == nil {
		return errors.New("enabled exporter composition is required")
	}
	return start(ctx, cfg)
}

func startEnabled(ctx context.Context, cfg config.Exporter) error {
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

	client, err := langfuse.NewHTTPClient(langfuse.HTTPConfig{
		BaseURL:         cfg.Langfuse.BaseURL,
		PublicKey:       cfg.Langfuse.PublicKey,
		SecretKey:       cfg.Langfuse.SecretKey,
		MaxRequestBytes: cfg.Langfuse.MaxRequestBytes,
	})
	if err != nil {
		return fmt.Errorf("create Langfuse client: %w", err)
	}
	store := postgres.NewStore(pool)
	exporter := &langfuse.Exporter{
		Repository: store,
		Inbox: langfuse.PostgresInbox{
			Deduplicator: consumer.NewDeduplicator(pool, "agenttrace-langfuse-exporter"),
		},
		RetryStore: langfuse.PostgresRetryStore{
			Store: store, Consumer: "agenttrace-langfuse-exporter",
		},
		Client:       client,
		SecretFields: cfg.Langfuse.SecretFields,
		MaxAttempts:  cfg.Langfuse.MaxAttempts,
	}

	relay := outbox.NewRelay(pool, bus, outbox.Config{WorkerID: processID("exporter-outbox")})
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

	if err := consumeLoop(ctx, exporter, bus, cfg.Langfuse.GroupID); err != nil &&
		!errors.Is(err, context.Canceled) {
		return fmt.Errorf("consume AgentTrace run events: %w", err)
	}
	return nil
}

func consumeLoop(
	ctx context.Context,
	exporter *langfuse.Exporter,
	bus eventbus.EventBus,
	group string,
) error {
	for {
		err := exporter.Consume(ctx, bus, group)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		log.Printf("Langfuse exporter consumer will retry: %v", err)
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func relayLoop(ctx context.Context, relay *outbox.Relay) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := relay.RelayOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("exporter outbox relay: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, os.Getpid())
}
