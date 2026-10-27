package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestPiRunnerReplayOfTerminalRunStartsNoProcess(t *testing.T) {
	store := &fakePiStore{startErr: postgres.ErrRunAlreadyTerminal}
	process := &fakeHarnessProcess{err: errors.New("should not run")}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-terminal-replay")); err != nil {
		t.Fatalf("replay of terminal run = %v, want nil", err)
	}
	if process.calls != 0 {
		t.Fatalf("process calls = %d, want 0 for terminal replay", process.calls)
	}
	if store.completed {
		t.Fatal("CompleteHarnessRun should not be called for terminal replay")
	}
}

func TestPiRunnerSucceedsWhenCasePassesBeforeSettled(t *testing.T) {
	events := passingEventStream("run-passed", "att-passed")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-1", RunID: "run-passed", AttemptNumber: 1, ProviderAttemptID: "att-passed",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-passed")); err != nil {
		t.Fatalf("passed run = %v, want nil", err)
	}
	if store.outcome.Status != domain.RunSucceeded {
		t.Fatalf("outcome status = %q, want succeeded", store.outcome.Status)
	}
	if store.outcome.Assistant != "final answer" {
		t.Fatalf("assistant = %q, want final answer", store.outcome.Assistant)
	}
	if len(store.events) != len(events) {
		t.Fatalf("persisted events = %d, want %d", len(store.events), len(events))
	}
}

func TestPiRunnerFailsWhenSettledWithoutPassedCase(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-no-pass", "att-no-pass", 1, `{}`),
		harEvent(piharness.EventCaseCompleted, "run-no-pass", "att-no-pass", 2, `{"verdict":"failed"}`),
		harEvent(piharness.EventSettled, "run-no-pass", "att-no-pass", 3, `{"assistant_text":"done"}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-2", RunID: "run-no-pass", AttemptNumber: 1, ProviderAttemptID: "att-no-pass",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-no-pass"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
}

func TestPiRunnerFailsOnHarnessFailedEvent(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-failed", "att-failed", 1, `{}`),
		harEvent(piharness.EventFailed, "run-failed", "att-failed", 2, `{"error":"boom"}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-3", RunID: "run-failed", AttemptNumber: 1, ProviderAttemptID: "att-failed",
	}}
	process := &fakeHarnessProcess{
		events: events,
		err:    &piharness.HarnessFailure{Message: "boom"},
	}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-failed"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
	if !strings.Contains(store.outcome.Error, "boom") {
		t.Fatalf("outcome error = %q, want boom", store.outcome.Error)
	}
}

func TestPiRunnerCancelledWhenDurableCancelRequestedEvenIfCasePasses(t *testing.T) {
	events := passingEventStream("run-cancel-durable", "att-cancel-durable")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-cd", RunID: "run-cancel-durable", AttemptNumber: 1,
		ProviderAttemptID: "att-cancel-durable",
		CancelRequested:   true,
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-cancel-durable"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError for durable cancel", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancelled (durable cancel overrides passed case)", store.outcome.Status)
	}
}

func TestPiRunnerCancelledOnContextCancellation(t *testing.T) {
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-4", RunID: "run-cancel", AttemptNumber: 1, ProviderAttemptID: "att-cancel",
	}}
	process := &fakeHarnessProcess{err: context.Canceled}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-cancel"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome status = %q, want cancelled", store.outcome.Status)
	}
}

func TestPiRunnerFailsOnPrematureExit(t *testing.T) {
	events := []piharness.Event{
		harEvent(piharness.EventStarted, "run-premature", "att-premature", 1, `{}`),
	}
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-5", RunID: "run-premature", AttemptNumber: 1, ProviderAttemptID: "att-premature",
	}}
	process := &fakeHarnessProcess{events: events, err: piharness.ErrPrematureExit}
	runner := PiRunner{Process: process, Store: store}

	err := runner.Run(context.Background(), commandFor("run-premature"))
	if !isTerminalRunError(err) {
		t.Fatalf("error = %v, want terminalRunError", err)
	}
	if store.outcome.Status != domain.RunFailed {
		t.Fatalf("outcome status = %q, want failed", store.outcome.Status)
	}
}

func TestPiRunnerPersistsEventsInOrder(t *testing.T) {
	events := passingEventStream("run-order", "att-order")
	store := &fakePiStore{ref: HarnessAttemptRef{
		ID: "hat-6", RunID: "run-order", AttemptNumber: 1, ProviderAttemptID: "att-order",
	}}
	process := &fakeHarnessProcess{events: events}
	runner := PiRunner{Process: process, Store: store}

	if err := runner.Run(context.Background(), commandFor("run-order")); err != nil {
		t.Fatalf("run = %v", err)
	}
	for i, ev := range events {
		if store.events[i].Sequence != ev.Sequence {
			t.Fatalf("event %d sequence = %d, want %d", i, store.events[i].Sequence, ev.Sequence)
		}
	}
}

func passingEventStream(runID, attemptID string) []piharness.Event {
	return []piharness.Event{
		harEvent(piharness.EventStarted, runID, attemptID, 1, `{}`),
		harEvent(piharness.EventMessageCompleted, runID, attemptID, 2, `{"text":"thinking"}`),
		harEvent(piharness.EventUsage, runID, attemptID, 3, `{"input_tokens":10,"output_tokens":5}`),
		harEvent(piharness.EventToolStarted, runID, attemptID, 4, `{"tool_call_id":"tc1","tool_name":"browser"}`),
		harEvent(piharness.EventObservation, runID, attemptID, 5, `{"observation":"page loaded"}`),
		harEvent(piharness.EventToolCompleted, runID, attemptID, 6, `{"tool_call_id":"tc1","tool_name":"browser","is_error":false}`),
		harEvent(piharness.EventCaseCompleted, runID, attemptID, 7, `{"verdict":"passed","steps":[{"action":"click"}]}`),
		harEvent(piharness.EventSettled, runID, attemptID, 8, `{"assistant_text":"final answer"}`),
	}
}

func harEvent(typ piharness.EventType, runID, attemptID string, seq int64, payload string) piharness.Event {
	return piharness.Event{
		Type:      typ,
		RunID:     runID,
		AttemptID: attemptID,
		Sequence:  seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   json.RawMessage(payload),
	}
}

type fakeHarnessProcess struct {
	events []piharness.Event
	err    error
	calls  int
}

func (f *fakeHarnessProcess) Run(_ context.Context, _ piharness.Request, onEvent func(piharness.Event)) error {
	f.calls++
	for _, ev := range f.events {
		onEvent(ev)
	}
	return f.err
}

type fakePiStore struct {
	ref       HarnessAttemptRef
	startErr  error
	events    []piharness.Event
	outcome   PiRunOutcome
	completed bool
}

func (f *fakePiStore) StartRun(_ context.Context, _ event.RunCommand) (HarnessAttemptRef, error) {
	if f.startErr != nil {
		return HarnessAttemptRef{}, f.startErr
	}
	return f.ref, nil
}

func (f *fakePiStore) AppendHarnessEvent(_ context.Context, _ HarnessAttemptRef, ev piharness.Event) error {
	f.events = append(f.events, ev)
	return nil
}

func (f *fakePiStore) CompleteHarnessRun(_ context.Context, _ HarnessAttemptRef, outcome PiRunOutcome) error {
	f.outcome = outcome
	f.completed = true
	return nil
}
