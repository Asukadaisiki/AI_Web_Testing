package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/export/langfuse"
)

func TestDisabledConfigurationStartsNoConsumerOrNetworkClients(t *testing.T) {
	started := false
	err := runConfigured(
		context.Background(),
		config.Exporter{Enabled: false},
		func(context.Context, config.Exporter) error {
			started = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("run disabled exporter: %v", err)
	}
	if started {
		t.Fatal("disabled exporter invoked enabled composition")
	}
}

func TestEnabledConfigurationRunsCompositionAndReturnsItsError(t *testing.T) {
	want := errors.New("consumer stopped")
	calls := 0
	err := runConfigured(
		context.Background(),
		config.Exporter{Enabled: true},
		func(context.Context, config.Exporter) error {
			calls++
			return want
		},
	)
	if !errors.Is(err, want) {
		t.Fatalf("run enabled exporter error = %v, want %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("enabled composition calls = %d, want 1", calls)
	}
}

func TestEnabledConfigurationPreservesExportProjectScope(t *testing.T) {
	err := runConfigured(context.Background(), config.Exporter{Enabled: true, ExportProjectID: "project_1"},
		func(_ context.Context, cfg config.Exporter) error {
			if cfg.ExportProjectID != "project_1" {
				t.Fatal("export project scope lost before composition")
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExporterConsumerNameUsesConfiguredKafkaGroupForDurableIdentity(t *testing.T) {
	const group = "agenttrace-langfuse-exporter-failure-isolated"
	name, err := exporterConsumerName(config.Exporter{
		Langfuse: config.Langfuse{GroupID: group},
	})
	if err != nil {
		t.Fatalf("resolve exporter consumer name: %v", err)
	}
	if name != group {
		t.Fatalf("consumer name = %q, want %q", name, group)
	}
}

func TestConsumeLoopStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := &blockingEventBus{started: make(chan eventbus.Message, 1)}
	exporter := &langfuse.Exporter{}
	done := make(chan error, 1)
	go func() {
		done <- consumeLoop(ctx, exporter, bus, "agenttrace-langfuse-exporter")
	}()

	select {
	case route := <-bus.started:
		if route.Topic != "agenttrace.run.events.v1" ||
			route.Key != "agenttrace-langfuse-exporter" {
			t.Fatalf("consumer route = (%q, %q)", route.Topic, route.Key)
		}
	case <-time.After(time.Second):
		t.Fatal("exporter consumer did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("consume loop error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("exporter consumer did not join after cancellation")
	}
}

type blockingEventBus struct {
	started chan eventbus.Message
}

func (b *blockingEventBus) Publish(
	context.Context,
	string,
	string,
	[]byte,
) (eventbus.Metadata, error) {
	return eventbus.Metadata{}, nil
}

func (b *blockingEventBus) Consume(
	ctx context.Context,
	topic string,
	group string,
	_ eventbus.Handler,
) error {
	b.started <- eventbus.Message{Topic: topic, Key: group}
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingEventBus) Close() error {
	return nil
}
