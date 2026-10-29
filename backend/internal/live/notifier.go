package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

const (
	runEventsTopic = "agenttrace.run.events.v1"
	notifierGroup  = "agenttrace-live-notifier"
)

// Notifier turns durable Kafka events into disposable Redis wakeups.
// Kafka envelopes have no PostgreSQL sequence, so their payloads are not SSE data.
type Notifier struct {
	Store         LiveStore
	RetryInterval time.Duration
	OnError       func(error)
}

// Handle acknowledges invalid notifications but retries failed store writes.
func (n *Notifier) Handle(ctx context.Context, message eventbus.Message) error {
	if n == nil || n.Store == nil {
		return errors.New("live notifier store is required")
	}
	var envelope event.Envelope[json.RawMessage]
	err := json.Unmarshal(message.Value, &envelope)
	if err == nil && (message.Topic != runEventsTopic ||
		envelope.SchemaVersion != "agenttrace.event.v1" ||
		strings.TrimSpace(envelope.EventID) == "" ||
		strings.TrimSpace(envelope.AggregateID) == "" ||
		strings.TrimSpace(envelope.EventType) == "") {
		err = errors.New("invalid run event notification envelope")
	}
	if err != nil {
		n.report(fmt.Errorf("discard run event notification: %w", err))
		return nil
	}
	if err := n.Store.Append(ctx, envelope.AggregateID, LiveEvent{
		Type: "run.events.available",
		Data: json.RawMessage(`{}`),
	}); err != nil {
		return fmt.Errorf("append run event wakeup: %w", err)
	}
	return nil
}

// Consume owns independent offsets and reconnects after an uncommitted failure.
func (n *Notifier) Consume(ctx context.Context, bus eventbus.EventBus) error {
	if n == nil || n.Store == nil || bus == nil {
		return errors.New("live notifier store and event bus are required")
	}
	interval := n.RetryInterval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := bus.Consume(ctx, runEventsTopic, notifierGroup, n.Handle)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			n.report(err)
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

func (n *Notifier) report(err error) {
	if n.OnError != nil {
		n.OnError(err)
	}
}
