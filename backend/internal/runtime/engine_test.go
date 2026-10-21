package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
)

func TestEnginePersistsModelAndOperationSpans(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-spans"))
	dispatcher := &fakeDispatcher{result: operation.Result{
		Status: "succeeded",
		Output: json.RawMessage(`{"url":"https://example.test"}`),
	}}
	engine := Engine{
		Model: model.NewScripted([]model.ScriptedStep{
			{Tool: "inspect", Arguments: json.RawMessage(`{"url":"https://example.test"}`)},
			{Final: "inspection complete"},
		}),
		Operations: dispatcher,
		Store:      store,
	}

	if err := engine.Run(context.Background(), commandFor("run-spans")); err != nil {
		t.Fatalf("run engine: %v", err)
	}

	if len(store.steps) != 3 {
		t.Fatalf("committed steps = %d, want 3", len(store.steps))
	}
	if store.steps[0].Kind != StepModel || store.steps[1].Kind != StepOperation || store.steps[2].Kind != StepModel {
		t.Fatalf("step kinds = %#v, want model, operation, model", stepKinds(store.steps))
	}
	if got, want := store.steps[0].LogicalSpanID, LogicalSpanID("run-spans", 0, StepModel); got != want {
		t.Fatalf("model logical span ID = %q, want %q", got, want)
	}
	if got, want := store.steps[1].LogicalSpanID, LogicalSpanID("run-spans", 1, StepOperation); got != want {
		t.Fatalf("operation logical span ID = %q, want %q", got, want)
	}
	if got, want := store.steps[1].AttemptID, AttemptID(store.steps[1].LogicalSpanID, 1); got != want {
		t.Fatalf("operation attempt ID = %q, want %q", got, want)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("operation dispatches = %d, want 1", dispatcher.calls)
	}
	if store.completion.Status != CompletionSucceeded {
		t.Fatalf("completion status = %q, want %q", store.completion.Status, CompletionSucceeded)
	}
}

func TestEngineStopsAtModelCallBudget(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-budget"))
	dispatcher := &fakeDispatcher{result: operation.Result{Status: "succeeded"}}
	engine := Engine{
		Model: model.NewScripted([]model.ScriptedStep{
			{Tool: "inspect", Arguments: json.RawMessage(`{}`)},
			{Final: "would exceed budget"},
		}),
		Operations:    dispatcher,
		Store:         store,
		MaxModelCalls: 1,
	}

	err := engine.Run(context.Background(), commandFor("run-budget"))
	if !errors.Is(err, ErrModelCallBudget) {
		t.Fatalf("run error = %v, want %v", err, ErrModelCallBudget)
	}
	if store.completion.Status != CompletionFailed {
		t.Fatalf("completion status = %q, want %q", store.completion.Status, CompletionFailed)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("operation dispatches = %d, want 1", dispatcher.calls)
	}
}

func TestEngineHonorsDurableCancelWithoutRedis(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-cancel"))
	store.checkpoint.CancelRequested = true
	engine := Engine{
		Model: model.NewScripted([]model.ScriptedStep{{Final: "must not be called"}}),
		Store: store,
	}

	err := engine.Run(context.Background(), commandFor("run-cancel"))
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("run error = %v, want %v", err, ErrCancelled)
	}
	if store.completion.Status != CompletionCancelled {
		t.Fatalf("completion status = %q, want %q", store.completion.Status, CompletionCancelled)
	}
	if len(store.steps) != 0 {
		t.Fatalf("committed steps = %d, want 0", len(store.steps))
	}
}

func TestEngineDoesNotTreatRedisCancelHintAsAuthoritative(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-redis-hint"))
	liveStore := live.NewMemoryStore()
	if err := liveStore.RequestCancel(context.Background(), "run-redis-hint", live.CancelTTL); err != nil {
		t.Fatalf("request redis cancel hint: %v", err)
	}
	engine := Engine{
		Model: model.NewScripted([]model.ScriptedStep{{Final: "durable state wins"}}),
		Store: store,
		Live:  liveStore,
	}

	if err := engine.Run(context.Background(), commandFor("run-redis-hint")); err != nil {
		t.Fatalf("run engine: %v", err)
	}
	if store.completion.Status != CompletionSucceeded {
		t.Fatalf("completion status = %q, want durable success", store.completion.Status)
	}
}

func TestDuplicateCommandDoesNotCreateSecondAssistantMessage(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-duplicate"))
	engine := Engine{
		Model: model.NewScripted([]model.ScriptedStep{{Final: "one durable answer"}}),
		Store: store,
	}
	command := commandFor("run-duplicate")

	if err := engine.Run(context.Background(), command); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := engine.Run(context.Background(), command); err != nil {
		t.Fatalf("duplicate run: %v", err)
	}

	if got := store.assistantMessages(); len(got) != 1 || got[0] != "one durable answer" {
		t.Fatalf("assistant messages = %#v, want one durable answer", got)
	}
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
