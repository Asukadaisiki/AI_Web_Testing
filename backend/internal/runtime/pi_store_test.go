package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestPostgresPiTraceStorePersistsFullEventStream(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "Pi trace"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi trace",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "inspect and report",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model", ReasoningEffort: "high"},
		IdempotencyKey: "pi-trace-full",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store}
	command := event.RunCommand{
		RunID:          run.ID,
		ConversationID: conversation.ID,
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model", ReasoningEffort: "high"},
		Messages: []domain.Message{{
			ID: "msg-user", ConversationID: conversation.ID, Ordinal: 1,
			Role: "user", Content: "inspect and report",
		}},
		WorkerURL:     "http://worker:8100",
		ArtifactScope: run.ID,
	}

	ref, err := piStore.StartRun(ctx, command)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if ref.AttemptNumber != 1 {
		t.Fatalf("attempt number = %d, want 1", ref.AttemptNumber)
	}

	events := passingEventStream(run.ID, ref.ProviderAttemptID)
	for _, ev := range events {
		if err := piStore.AppendHarnessEvent(ctx, ref, ev); err != nil {
			t.Fatalf("append event %d: %v", ev.Sequence, err)
		}
	}

	if err := piStore.CompleteHarnessRun(ctx, ref, PiRunOutcome{
		Status:    domain.RunSucceeded,
		Assistant: "final answer",
	}); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	finished, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if finished.Status != domain.RunSucceeded {
		t.Fatalf("run status = %q, want succeeded", finished.Status)
	}

	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	var hasModel, hasTool, hasRoot bool
	for _, span := range details.Spans {
		switch span.Kind {
		case domain.SpanRun:
			hasRoot = true
		case domain.SpanModel:
			hasModel = true
			if !strings.Contains(string(span.Output), "thinking") {
				t.Fatalf("model span output = %s, want message payload", span.Output)
			}
		case domain.SpanOperation:
			hasTool = true
		}
	}
	if !hasRoot || !hasModel || !hasTool {
		t.Fatalf("trace missing spans: root=%v model=%v tool=%v", hasRoot, hasModel, hasTool)
	}

	var hasUsage bool
	for _, u := range details.Usage {
		if u.InputTokens == 10 && u.OutputTokens == 5 {
			hasUsage = true
		}
	}
	if !hasUsage {
		t.Fatalf("usage record not found in %#v", details.Usage)
	}

	var hasCaseOp bool
	for _, op := range details.Operations {
		if op.Status == "passed" {
			hasCaseOp = true
		}
	}
	if !hasCaseOp {
		t.Fatalf("case operation not found in %#v", details.Operations)
	}
}

func TestPostgresPiTraceStoreStartRunUsesCommandCheckpointVersion(t *testing.T) {
	ctx, pool, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "Pi checkpoint"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi checkpoint",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "checkpoint test",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-checkpoint-match",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Advance the run's checkpoint as if the engine path had progressed it.
	if _, err := pool.Exec(ctx, `UPDATE runs SET checkpoint_version = 5 WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("advance checkpoint: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store}
	command := event.RunCommand{
		RunID:            run.ID,
		ConversationID:   conversation.ID,
		Model:            event.ModelRef{Provider: "pi", Name: "browser-model"},
		Messages:         []domain.Message{{ID: "m", ConversationID: conversation.ID, Ordinal: 1, Role: "user", Content: "go"}},
		CheckpointVersion: 5,
	}
	if _, err := piStore.StartRun(ctx, command); err != nil {
		t.Fatalf("start run with matching checkpoint = %v, want nil", err)
	}

	// A second run with a stale checkpoint must conflict rather than silently
	// create the root span against an unexpected checkpoint.
	_, run2, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "checkpoint mismatch",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-checkpoint-mismatch",
	})
	if err != nil {
		t.Fatalf("create run 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET checkpoint_version = 7 WHERE id = $1`, run2.ID); err != nil {
		t.Fatalf("advance run 2 checkpoint: %v", err)
	}
	command2 := command
	command2.RunID = run2.ID
	command2.CheckpointVersion = 0
	_, err = piStore.StartRun(ctx, command2)
	if !errors.Is(err, postgres.ErrConflict) {
		t.Fatalf("start run with mismatched checkpoint = %v, want ErrConflict", err)
	}
}

func TestPostgresPiTraceStoreCancelledRunPersistsCancelledState(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "Pi cancel"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi cancel",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "cancel me",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-cancel",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, command)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := piStore.AppendHarnessEvent(ctx, ref, harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`)); err != nil {
		t.Fatalf("append started: %v", err)
	}
	if err := piStore.CompleteHarnessRun(ctx, ref, PiRunOutcome{
		Status: domain.RunCancelled,
		Error:  "context canceled",
	}); err != nil {
		t.Fatalf("complete cancelled: %v", err)
	}
	finished, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if finished.Status != domain.RunCancelled {
		t.Fatalf("run status = %q, want cancelled", finished.Status)
	}
}
