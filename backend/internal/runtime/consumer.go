package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
)

const (
	runCommandTopic  = "agenttrace.run.commands.v1"
	runCommandSchema = "agenttrace.run-command.v1"
	defaultDLQTopic  = "agenttrace.run.dlq.v1"
)

// CommandRunner processes one immutable run command.
type CommandRunner interface {
	Run(context.Context, event.RunCommand) error
}

// Inbox claims a delivered message before its durable effects execute.
type Inbox interface {
	Handle(context.Context, eventbus.Message, func(context.Context) error) error
}

// RetryStore records failed deliveries independently of a Consumer process.
type RetryStore interface {
	Increment(context.Context, string, []byte) (int, error)
	Clear(context.Context, string) error
	HandoffDLQ(context.Context, string, DLQHandoff) error
	HasDLQHandoff(context.Context, string, string) (bool, error)
}

// DLQHandoff describes an idempotent, durable transfer to the DLQ outbox.
type DLQHandoff struct {
	EventID string
	Topic   string
	Key     string
	Payload []byte
}

// DeduplicatedInbox adapts the Task 4 PostgreSQL inbox primitive.
type DeduplicatedInbox struct {
	Deduplicator *inboxconsumer.Deduplicator
}

// Handle applies handler only to the first successful event ID delivery.
func (i DeduplicatedInbox) Handle(
	ctx context.Context,
	message eventbus.Message,
	handler func(context.Context) error,
) error {
	if i.Deduplicator == nil {
		return errors.New("runtime inbox deduplicator is required")
	}
	return i.Deduplicator.Handle(ctx, message, func(ctx context.Context, _ inboxconsumer.Tx, _ eventbus.Message) error {
		return handler(ctx)
	})
}

// Consumer accepts one versioned Kafka command and invokes a checkpointed engine.
type Consumer struct {
	Inbox       Inbox
	Runner      CommandRunner
	Publisher   eventbus.EventBus
	RetryStore  RetryStore
	Live        live.LiveStore
	LeaseOwner  string
	LeaseTTL    time.Duration
	MaxAttempts int
	DLQTopic    string

	mu       sync.Mutex
	attempts map[string]int
}

// Consume subscribes this runtime to the sole accepted command topic.
func (c *Consumer) Consume(ctx context.Context, bus eventbus.EventBus, group string) error {
	if bus == nil {
		return errors.New("runtime event bus is required")
	}
	if strings.TrimSpace(group) == "" {
		return errors.New("runtime consumer group is required")
	}
	return bus.Consume(ctx, runCommandTopic, group, c.Handle)
}

// Handle processes one Kafka record. It accepts only agenttrace.run-command.v1.
func (c *Consumer) Handle(ctx context.Context, message eventbus.Message) error {
	if message.Topic != "" && message.Topic != runCommandTopic {
		return c.recordFailure(ctx, "", message, fmt.Errorf("unsupported command topic %q", message.Topic))
	}
	command, eventID, normalizedMessage, err := decodeRunCommand(message)
	if err != nil {
		return c.recordFailure(ctx, eventID, message, err)
	}
	handedOff, err := c.hasDLQHandoff(ctx, eventID)
	if err != nil {
		return fmt.Errorf("check run command DLQ handoff: %w", err)
	}
	if handedOff {
		return nil
	}
	if c.Inbox == nil {
		return c.recordFailure(ctx, eventID, message, errors.New("runtime inbox is required"))
	}
	if c.Runner == nil {
		return c.recordFailure(ctx, eventID, message, errors.New("runtime command runner is required"))
	}
	err = c.Inbox.Handle(ctx, normalizedMessage, func(runCtx context.Context) error {
		stopLease := c.renewLease(runCtx, command.RunID)
		defer stopLease()
		return c.Runner.Run(runCtx, command)
	})
	if err != nil {
		return c.recordFailure(ctx, eventID, message, err)
	}
	if err := c.clearAttempts(ctx, eventID); err != nil {
		return fmt.Errorf("clear run command retry count: %w", err)
	}
	return nil
}

func decodeRunCommand(message eventbus.Message) (event.RunCommand, string, eventbus.Message, error) {
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		return event.RunCommand{}, "", message, fmt.Errorf("decode run command: %w", err)
	}
	if envelope.SchemaVersion != runCommandSchema {
		return event.RunCommand{}, envelope.EventID, message, fmt.Errorf("unsupported run command schema %q", envelope.SchemaVersion)
	}
	var command event.RunCommand
	if err := json.Unmarshal(envelope.Payload, &command); err != nil {
		return event.RunCommand{}, envelope.EventID, message, fmt.Errorf("decode run command payload: %w", err)
	}
	if strings.TrimSpace(command.RunID) == "" {
		return event.RunCommand{}, envelope.EventID, message, errors.New("run command run id is required")
	}
	eventID := retryEventID(envelope.EventID, message)
	if envelope.EventID != eventID {
		envelope.EventID = eventID
		normalized, err := json.Marshal(envelope)
		if err != nil {
			return event.RunCommand{}, eventID, message, fmt.Errorf("normalize run command event id: %w", err)
		}
		message.Value = normalized
	}
	return command, eventID, message, nil
}

func (c *Consumer) recordFailure(ctx context.Context, eventID string, message eventbus.Message, cause error) error {
	eventID = retryEventID(eventID, message)
	handedOff, err := c.hasDLQHandoff(ctx, eventID)
	if err != nil {
		return fmt.Errorf("check run command DLQ handoff: %w", err)
	}
	if handedOff {
		return nil
	}
	attempt, err := c.incrementAttempts(ctx, eventID, message.Value)
	if err != nil {
		return fmt.Errorf("record run command failure: %w", err)
	}
	if attempt < c.maxAttempts() {
		return cause
	}
	original := any(json.RawMessage(message.Value))
	if !json.Valid(message.Value) {
		original = string(message.Value)
	}
	payload, err := json.Marshal(map[string]any{
		"original": original,
		"error":    cause.Error(),
		"attempt":  attempt,
	})
	if err != nil {
		return fmt.Errorf("marshal run command DLQ: %w", err)
	}
	if c.RetryStore != nil {
		handoff := DLQHandoff{
			EventID: stableID("consumer-dlq", eventID),
			Topic:   c.dlqTopic(),
			Key:     message.Key,
			Payload: payload,
		}
		if err := c.RetryStore.HandoffDLQ(ctx, eventID, handoff); err != nil {
			return fmt.Errorf("handoff run command DLQ: %w", err)
		}
		return nil
	}
	if c.Publisher == nil {
		return fmt.Errorf("run command failed after %d attempts: %w", attempt, cause)
	}
	if _, err := c.Publisher.Publish(ctx, c.dlqTopic(), message.Key, payload); err != nil {
		return fmt.Errorf("publish run command DLQ: %w", err)
	}
	if err := c.clearAttempts(ctx, eventID); err != nil {
		return fmt.Errorf("clear run command retry count after DLQ: %w", err)
	}
	return nil
}

func retryEventID(eventID string, message eventbus.Message) string {
	if normalized := strings.TrimSpace(eventID); normalized != "" {
		return normalized
	}
	return stableID("consumer-retry", message.Topic, message.Key, string(message.Value))
}

func (c *Consumer) renewLease(ctx context.Context, runID string) func() {
	if c.Live == nil || strings.TrimSpace(c.LeaseOwner) == "" {
		return func() {}
	}
	ttl := c.LeaseTTL
	if ttl <= 0 {
		ttl = live.LeaseTTL
	}
	_ = c.Live.SetLease(ctx, runID, c.LeaseOwner, ttl)
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(maxDuration(ttl/2, time.Second))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				_ = c.Live.SetLease(ctx, runID, c.LeaseOwner, ttl)
			}
		}
	}()
	return func() { close(stop) }
}

func (c *Consumer) incrementAttempts(ctx context.Context, eventID string, payload []byte) (int, error) {
	if c.RetryStore != nil {
		return c.RetryStore.Increment(ctx, eventID, payload)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.attempts == nil {
		c.attempts = make(map[string]int)
	}
	c.attempts[eventID]++
	return c.attempts[eventID], nil
}

func (c *Consumer) clearAttempts(ctx context.Context, eventID string) error {
	if c.RetryStore != nil {
		return c.RetryStore.Clear(ctx, eventID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.attempts, eventID)
	return nil
}

func (c *Consumer) hasDLQHandoff(ctx context.Context, eventID string) (bool, error) {
	if c.RetryStore == nil {
		return false, nil
	}
	return c.RetryStore.HasDLQHandoff(ctx, eventID, stableID("consumer-dlq", eventID))
}

func (c *Consumer) maxAttempts() int {
	if c.MaxAttempts <= 0 {
		return 3
	}
	return c.MaxAttempts
}

func (c *Consumer) dlqTopic() string {
	if c.DLQTopic == "" {
		return defaultDLQTopic
	}
	return c.DLQTopic
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
