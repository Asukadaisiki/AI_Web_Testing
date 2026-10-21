package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
)

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

type recordingRunner struct {
	calls int
	err   error
}

func (r *recordingRunner) Run(_ context.Context, _ event.RunCommand) error {
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
