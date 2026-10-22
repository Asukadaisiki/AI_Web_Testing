package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
)

func TestEngineResumesAfterCompletedOperation(t *testing.T) {
	store := newMemoryCheckpointStore(commandFor("run-resume"))
	dispatcher := &fakeDispatcher{result: operation.Result{
		Status: "succeeded",
		Output: json.RawMessage(`{"title":"home"}`),
	}}
	first := Engine{
		Model: model.NewScripted([]model.ScriptedStep{
			{Tool: "inspect", Arguments: json.RawMessage(`{"selector":"h1"}`)},
		}),
		Operations: dispatcher,
		Store:      store,
	}
	store.failAfterCommits = 2

	if err := first.Run(context.Background(), commandFor("run-resume")); !errors.Is(err, errInjectedCrash) {
		t.Fatalf("first run error = %v, want injected crash", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("first engine dispatches = %d, want 1", dispatcher.calls)
	}
	if got := store.checkpoint.NextCheckpoint; got != 2 {
		t.Fatalf("checkpoint after durable operation = %d, want 2", got)
	}

	store.failAfterCommits = 0
	second := Engine{
		Model:      model.NewScripted([]model.ScriptedStep{{Final: "resumed answer"}}),
		Operations: dispatcher,
		Store:      store,
	}
	if err := second.Run(context.Background(), commandFor("run-resume")); err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("resumed engine redispatched completed operation %d times", dispatcher.calls)
	}
	if got := store.assistantMessages(); len(got) != 1 || got[0] != "resumed answer" {
		t.Fatalf("assistant messages = %#v, want resumed answer", got)
	}
}

func TestInitialMessagesPreservesPersistedResponseAfterCheckpoint(t *testing.T) {
	checkpoint := Checkpoint{
		Command: event.RunCommand{
			Messages: []domain.Message{
				{Role: "user", Content: "initial request"},
				{Role: "user", Content: "approved"},
			},
		},
		Messages: []model.Message{
			{Role: model.RoleAssistant, Content: "need approval"},
			{Role: model.RoleTool, Content: `{"status":"waiting"}`, ToolCallID: "call_1"},
		},
	}
	messages := initialMessages(checkpoint)
	if len(messages) != 4 {
		t.Fatalf("messages = %#v, want initial, response, and checkpoint history", messages)
	}
	if messages[0].Content != "initial request" ||
		messages[1].Content != "approved" ||
		messages[2].Content != "need approval" ||
		messages[3].ToolCallID != "call_1" {
		t.Fatalf("reconstructed messages = %#v", messages)
	}
}

var errInjectedCrash = errors.New("injected process crash")

type fakeDispatcher struct {
	calls  int
	result operation.Result
	err    error
}

func (d *fakeDispatcher) Dispatch(_ context.Context, _ operation.Call) (operation.Result, error) {
	d.calls++
	return d.result, d.err
}

type memoryCheckpointStore struct {
	mu               sync.Mutex
	checkpoint       Checkpoint
	steps            []StepResult
	started          []StepResult
	completion       Completion
	commits          int
	failAfterCommits int
	crashAfterStart  bool
}

func newMemoryCheckpointStore(command event.RunCommand) *memoryCheckpointStore {
	return &memoryCheckpointStore{checkpoint: Checkpoint{RunID: command.RunID, Command: command}}
}

func (s *memoryCheckpointStore) Load(_ context.Context, runID string) (Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.checkpoint.RunID {
		return Checkpoint{}, errors.New("unknown run")
	}
	if s.failAfterCommits > 0 && s.commits >= s.failAfterCommits {
		return Checkpoint{}, errInjectedCrash
	}
	return s.clone(), nil
}

func (s *memoryCheckpointStore) CommitStep(_ context.Context, step StepResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if step.RunID != s.checkpoint.RunID || step.Checkpoint != s.checkpoint.NextCheckpoint {
		return errors.New("checkpoint conflict")
	}
	s.steps = append(s.steps, step)
	s.commits++
	s.checkpoint.NextCheckpoint++
	if step.Kind == StepModel || step.Kind == StepReport {
		s.checkpoint.ModelCalls++
		s.checkpoint.Messages = append(s.checkpoint.Messages, step.Model.Message)
		s.checkpoint.PendingOperation = step.OperationCall
	}
	if step.Kind == StepOperation {
		s.checkpoint.Messages = append(s.checkpoint.Messages, model.Message{
			Role:       model.RoleTool,
			Content:    string(step.OperationResult.Output),
			ToolCallID: step.OperationCall.ToolCallID,
		})
		s.checkpoint.PendingOperation = nil
	}
	return nil
}

func (s *memoryCheckpointStore) StartAttempt(_ context.Context, attempt StepResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt.RunID != s.checkpoint.RunID || attempt.Checkpoint != s.checkpoint.NextCheckpoint {
		return errors.New("checkpoint conflict")
	}
	for _, started := range s.started {
		if started.AttemptID == attempt.AttemptID {
			if s.crashAfterStart {
				return errInjectedCrash
			}
			return nil
		}
	}
	s.started = append(s.started, attempt)
	if s.crashAfterStart {
		return errInjectedCrash
	}
	return nil
}

func (s *memoryCheckpointStore) CompleteRun(_ context.Context, completion Completion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completion.Status != "" {
		return nil
	}
	s.completion = completion
	s.checkpoint.Completed = true
	return nil
}

func (s *memoryCheckpointStore) assistantMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var messages []string
	for _, step := range s.steps {
		if step.Kind == StepModel && step.Model.Message.Content != "" {
			messages = append(messages, step.Model.Message.Content)
		}
	}
	return messages
}

func (s *memoryCheckpointStore) clone() Checkpoint {
	out := s.checkpoint
	out.Messages = append([]model.Message(nil), s.checkpoint.Messages...)
	if s.checkpoint.PendingOperation != nil {
		pending := *s.checkpoint.PendingOperation
		pending.Arguments = append(json.RawMessage(nil), pending.Arguments...)
		out.PendingOperation = &pending
	}
	return out
}

func (s *memoryCheckpointStore) currentCheckpoint() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoint.NextCheckpoint
}

func (s *memoryCheckpointStore) hasStarted(checkpoint int64, kind StepKind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, attempt := range s.started {
		if attempt.Checkpoint == checkpoint && attempt.Kind == kind {
			return true
		}
	}
	return false
}

func stepKinds(steps []StepResult) []StepKind {
	kinds := make([]StepKind, len(steps))
	for i, step := range steps {
		kinds[i] = step.Kind
	}
	return kinds
}
