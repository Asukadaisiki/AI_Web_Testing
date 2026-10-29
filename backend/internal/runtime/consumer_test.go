package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	inboxconsumer "github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

func TestConsumerDefersTemporaryRunnerContentionWithoutSpendingRetries(t *testing.T) {
	cause := errors.New("scheduler capacity busy")
	deferred := &temporaryDeliveryError{cause: cause}
	retries := &memoryRetryStore{}
	c := Consumer{
		Inbox: &memoryInbox{}, Runner: &recordingRunner{err: deferred},
		RetryStore: retries, MaxAttempts: 1,
	}
	if err := c.Handle(context.Background(), commandMessage(t, "evt-deferred", commandFor("run-deferred"))); !errors.Is(err, cause) {
		t.Fatalf("deferred delivery = %v, want original contention cause", err)
	}
	if len(retries.incremented) != 0 || retries.handoffCount != 0 {
		t.Fatalf("deferred delivery spent retries: %#v", retries)
	}
}

func TestConsumerConsumeWaitsForDeferredClaimWithoutStoppingBus(t *testing.T) {
	calls := 0
	runner := &recordingRunner{}
	retries := &memoryRetryStore{}
	c := Consumer{
		Inbox: inboxHandleFunc(func(ctx context.Context, _ eventbus.Message, handler func(context.Context) error) error {
			calls++
			if calls == 1 {
				return inboxconsumer.ErrClaimInProgress
			}
			return handler(ctx)
		}),
		Runner: runner, RetryStore: retries, MaxAttempts: 1,
	}
	bus := &singleDeliveryBus{
		Fake: &eventbus.Fake{}, message: commandMessage(t, "evt-deferred-consume", commandFor("run-deferred-consume")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Consume(ctx, bus, "runtime"); err != nil {
		t.Fatalf("consume active claim: %v", err)
	}
	if runner.calls != 1 || calls != 2 || len(retries.incremented) != 0 {
		t.Fatalf("runner/inbox/retries = %d/%d/%d, want 1/2/0", runner.calls, calls, len(retries.incremented))
	}
}

func TestConsumerConsumeDeferredWaitHonorsCancellation(t *testing.T) {
	c := Consumer{
		Inbox: inboxHandleFunc(func(context.Context, eventbus.Message, func(context.Context) error) error {
			return inboxconsumer.ErrClaimInProgress
		}),
		Runner: &recordingRunner{},
	}
	bus := &singleDeliveryBus{
		Fake: &eventbus.Fake{}, message: commandMessage(t, "evt-deferred-cancel", commandFor("run-deferred-cancel")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Consume(ctx, bus, "runtime"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deferred cancellation = %v, want context deadline", err)
	}
}

type temporaryDeliveryError struct{ cause error }

func (e *temporaryDeliveryError) Error() string          { return e.cause.Error() }
func (e *temporaryDeliveryError) Unwrap() error          { return e.cause }
func (e *temporaryDeliveryError) DeliveryDeferred() bool { return true }

type inboxHandleFunc func(context.Context, eventbus.Message, func(context.Context) error) error

func (f inboxHandleFunc) Handle(ctx context.Context, m eventbus.Message, h func(context.Context) error) error {
	return f(ctx, m, h)
}

type singleDeliveryBus struct {
	*eventbus.Fake
	message eventbus.Message
}

func (b *singleDeliveryBus) Consume(ctx context.Context, _ string, _ string, handler eventbus.Handler) error {
	return handler(ctx, b.message)
}

func TestConsumerRunsOnlyAgentTraceRunCommandsOnce(t *testing.T) {
	runner := &recordingRunner{}
	inbox := &memoryInbox{}
	consumer := Consumer{
		Inbox:  inbox,
		Runner: runner,
	}
	message := commandMessage(t, "evt-command", commandFor("run-consumer"))

	if err := consumer.Handle(context.Background(), message); err != nil {
		t.Fatalf("first command: %v", err)
	}
	if err := consumer.Handle(context.Background(), message); err != nil {
		t.Fatalf("duplicate command: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}

	message.Value = mustJSON(t, event.Envelope[event.RunCommand]{
		SchemaVersion: "agenttrace.other-command.v1",
		EventID:       "evt-other",
		EventType:     "run.requested",
		AggregateID:   "run-other",
		OccurredAt:    time.Now().UTC(),
		CorrelationID: "run-other",
		Payload:       commandFor("run-other"),
	})
	if err := consumer.Handle(context.Background(), message); err == nil {
		t.Fatal("accepted unsupported command schema")
	}
}

func TestConsumerCommitsInboxForDurablyCompletedRunFailure(t *testing.T) {
	cause := errors.New("operation callback unavailable")
	runner := &recordingRunner{err: &terminalRunError{cause: cause}}
	inbox := &memoryInbox{}
	retries := &memoryRetryStore{}
	consumer := Consumer{
		Inbox:       inbox,
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	message := commandMessage(t, "evt-terminal-run-failure", commandFor("run-terminal-failure"))

	if err := consumer.Handle(context.Background(), message); err != nil {
		t.Fatalf("terminal run failure: %v", err)
	}
	if err := consumer.Handle(context.Background(), message); err != nil {
		t.Fatalf("terminal run failure replay: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want one committed inbox delivery", runner.calls)
	}
	if len(retries.incremented) != 0 {
		t.Fatalf("retry increments = %#v, want none for durable terminal failure", retries.incremented)
	}
}

func TestConsumerSendsPermanentFailuresToDLQAfterLimit(t *testing.T) {
	runner := &recordingRunner{err: errors.New("permanent failure")}
	bus := &eventbus.Fake{}
	consumer := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		Publisher:   bus,
		MaxAttempts: 2,
	}
	message := commandMessage(t, "evt-fail", commandFor("run-dlq"))

	if err := consumer.Handle(context.Background(), message); err == nil {
		t.Fatal("first failure returned nil")
	}
	if err := consumer.Handle(context.Background(), message); err != nil {
		t.Fatalf("DLQ escalation: %v", err)
	}
	published := bus.Published()
	if len(published) != 1 {
		t.Fatalf("published messages = %d, want 1", len(published))
	}
	if published[0].Topic != "agenttrace.run.dlq.v1" {
		t.Fatalf("DLQ topic = %q", published[0].Topic)
	}
}

func TestConsumerRoutesFailureToDLQAcrossFreshInstances(t *testing.T) {
	runner := &recordingRunner{err: errors.New("permanent failure")}
	bus := &eventbus.Fake{}
	retries := &memoryRetryStore{}
	message := commandMessage(t, "evt-durable-fail", commandFor("run-durable-dlq"))

	first := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		Publisher:   bus,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := first.Handle(context.Background(), message); err == nil {
		t.Fatal("first failure returned nil")
	}

	second := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		Publisher:   bus,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := second.Handle(context.Background(), message); err != nil {
		t.Fatalf("durable second failure must escalate to DLQ: %v", err)
	}
	if retries.handoffCount != 1 {
		t.Fatalf("durable DLQ handoffs = %d, want one", retries.handoffCount)
	}
	if len(bus.Published()) != 0 {
		t.Fatalf("direct DLQ publishes = %#v, want none with durable retry state", bus.Published())
	}
}

func TestConsumerRoutesMalformedCommandsToDLQAfterDurableLimit(t *testing.T) {
	bus := &eventbus.Fake{}
	retries := &memoryRetryStore{}
	message := eventbus.Message{
		Topic: runCommandTopic,
		Key:   "malformed",
		Value: []byte(`{"schema_version":`),
	}

	first := Consumer{Publisher: bus, RetryStore: retries, MaxAttempts: 2}
	if err := first.Handle(context.Background(), message); err == nil {
		t.Fatal("first malformed command returned nil")
	}
	second := Consumer{Publisher: bus, RetryStore: retries, MaxAttempts: 2}
	if err := second.Handle(context.Background(), message); err != nil {
		t.Fatalf("malformed command must escalate to DLQ: %v", err)
	}
	if retries.handoffCount != 1 {
		t.Fatalf("durable DLQ handoffs = %d, want one", retries.handoffCount)
	}
	if len(bus.Published()) != 0 {
		t.Fatalf("direct DLQ publishes = %#v, want none with durable retry state", bus.Published())
	}
	fallbackID := stableID("consumer-retry", message.Topic, message.Key, string(message.Value))
	handoff, ok := retries.dlq[fallbackID]
	if !ok {
		t.Fatalf("missing durable DLQ handoff for %q", fallbackID)
	}
	var payload map[string]any
	if err := json.Unmarshal(handoff.Payload, &payload); err != nil {
		t.Fatalf("DLQ payload must be valid JSON: %v", err)
	}
	if got, ok := payload["original"].(string); !ok || got != string(message.Value) {
		t.Fatalf("DLQ original = %#v, want malformed source text", payload["original"])
	}
}

func TestConsumerDerivesStableFallbackForWhitespaceEnvelopeIDAtDurableLimit(t *testing.T) {
	runner := &recordingRunner{err: errors.New("permanent failure")}
	bus := &eventbus.Fake{}
	retries := &memoryRetryStore{}
	message := commandMessage(t, " \t ", commandFor("run-whitespace-event-id"))
	wantID := stableID("consumer-retry", message.Topic, message.Key, string(message.Value))

	first := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		Publisher:   bus,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := first.Handle(context.Background(), message); err == nil {
		t.Fatal("first whitespace event ID failure returned nil")
	}
	second := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		Publisher:   bus,
		RetryStore:  retries,
		MaxAttempts: 2,
	}
	if err := second.Handle(context.Background(), message); err != nil {
		t.Fatalf("whitespace event ID must reach durable limit: %v", err)
	}
	if len(retries.incremented) != 2 {
		t.Fatalf("recorded retry IDs = %#v, want two attempts", retries.incremented)
	}
	for _, eventID := range retries.incremented {
		if eventID != wantID {
			t.Fatalf("retry event ID = %q, want stable fallback %q", eventID, wantID)
		}
	}
	if retries.handoffCount != 1 {
		t.Fatalf("durable DLQ handoffs = %d, want one", retries.handoffCount)
	}
}

func TestConsumerConcurrentFreshInstancesCreateOneDLQHandoff(t *testing.T) {
	runner := &recordingRunner{err: errors.New("permanent failure")}
	retries := &memoryRetryStore{}
	message := commandMessage(t, "evt-concurrent-dlq", commandFor("run-concurrent-dlq"))
	start := make(chan struct{})
	errs := make(chan error, 2)

	for range 2 {
		consumer := Consumer{
			Inbox:       &memoryInbox{},
			Runner:      runner,
			RetryStore:  retries,
			MaxAttempts: 1,
		}
		go func() {
			<-start
			errs <- consumer.Handle(context.Background(), message)
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent durable DLQ handoff: %v", err)
		}
	}
	if retries.handoffCount != 1 {
		t.Fatalf("durable DLQ handoffs = %d, want one", retries.handoffCount)
	}
}

func TestConsumerReplayAfterDLQHandoffDoesNotCreateAnotherHandoff(t *testing.T) {
	runner := &recordingRunner{err: errors.New("permanent failure")}
	retries := &memoryRetryStore{}
	message := commandMessage(t, "evt-dlq-replay", commandFor("run-dlq-replay"))

	first := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 1,
	}
	if err := first.Handle(context.Background(), message); err != nil {
		t.Fatalf("initial durable DLQ handoff: %v", err)
	}
	replay := Consumer{
		Inbox:       &memoryInbox{},
		Runner:      runner,
		RetryStore:  retries,
		MaxAttempts: 1,
	}
	if err := replay.Handle(context.Background(), message); err != nil {
		t.Fatalf("replay after durable DLQ handoff: %v", err)
	}
	if retries.handoffCount != 1 {
		t.Fatalf("durable DLQ handoffs after replay = %d, want one", retries.handoffCount)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls after replay = %d, want one", runner.calls)
	}
}

type recordingRunner struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (r *recordingRunner) Run(_ context.Context, _ event.RunCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.err
}

type memoryInbox struct {
	seen map[string]bool
}

func (i *memoryInbox) Handle(_ context.Context, message eventbus.Message, handle func(context.Context) error) error {
	if i.seen == nil {
		i.seen = map[string]bool{}
	}
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		return err
	}
	if i.seen[envelope.EventID] {
		return nil
	}
	if err := handle(context.Background()); err != nil {
		return err
	}
	i.seen[envelope.EventID] = true
	return nil
}

type memoryRetryStore struct {
	mu           sync.Mutex
	attempts     map[string]int
	incremented  []string
	handoffCount int
	dlq          map[string]DLQHandoff
}

func (s *memoryRetryStore) Increment(_ context.Context, eventID string, _ []byte) (RetryIncrement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, handedOff := s.dlq[eventID]; handedOff {
		return RetryIncrement{DLQHandedOff: true}, nil
	}
	if s.attempts == nil {
		s.attempts = make(map[string]int)
	}
	s.incremented = append(s.incremented, eventID)
	s.attempts[eventID]++
	return RetryIncrement{Attempts: s.attempts[eventID]}, nil
}

func (s *memoryRetryStore) Clear(_ context.Context, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, eventID)
	return nil
}

func (s *memoryRetryStore) HandoffDLQ(_ context.Context, eventID string, handoff DLQHandoff) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dlq == nil {
		s.dlq = make(map[string]DLQHandoff)
	}
	if _, exists := s.dlq[eventID]; exists {
		return nil
	}
	handoff.Payload = append([]byte(nil), handoff.Payload...)
	s.dlq[eventID] = handoff
	s.handoffCount++
	delete(s.attempts, eventID)
	return nil
}

func (s *memoryRetryStore) HasDLQHandoff(_ context.Context, eventID string, outboxEventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handoff, exists := s.dlq[eventID]
	return exists && handoff.EventID == outboxEventID, nil
}

func commandMessage(t *testing.T, eventID string, command event.RunCommand) eventbus.Message {
	t.Helper()
	return eventbus.Message{
		Topic: "agenttrace.run.commands.v1",
		Key:   command.RunID,
		Value: mustJSON(t, event.Envelope[event.RunCommand]{
			SchemaVersion: "agenttrace.run-command.v1",
			EventID:       eventID,
			EventType:     "run.requested",
			AggregateID:   command.RunID,
			OccurredAt:    time.Now().UTC(),
			CorrelationID: command.RunID,
			Payload:       command,
		}),
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}

func commandFor(runID string) event.RunCommand {
	return event.RunCommand{
		RunID:          runID,
		ConversationID: "conversation-1",
		Model:          event.ModelRef{Provider: "scripted", Name: "fixture"},
		Messages: []domain.Message{{
			ID:             "msg-user",
			ConversationID: "conversation-1",
			Ordinal:        1,
			Role:           "user",
			Content:        "Inspect the page",
		}},
	}
}
