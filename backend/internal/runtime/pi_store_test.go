package runtime

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestPostgresPiTraceStorePersistsFinishCaseEvidence(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, postgres.CreateProjectParams{Name: "Pi finish evidence"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi finish evidence",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "finish the browser case",
		AgentID:        "browser-eval",
		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-finish-evidence",
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

	const finishTool = "mcp__browser_harness__finish_case"
	events := []piharness.Event{
		harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`),
		harEvent(piharness.EventToolStarted, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"finish-1","tool_name":"`+finishTool+`"}`),
		harEvent(piharness.EventToolCompleted, run.ID, ref.ProviderAttemptID, 3,
			`{"tool_call_id":"finish-1","tool_name":"`+finishTool+`","is_error":false}`),
		harEvent(piharness.EventCaseCompleted, run.ID, ref.ProviderAttemptID, 4, `{
			"verdict":"passed",
			"case":{"case_version":"loop.case.v1","steps":[{"action":"goto"}]},
			"dry_run":{
				"status":"passed",
				"execution_id":"exec-1",
				"steps":[
					{"index":0,"action":"goto","status":"passed","started_at":"2026-10-26T12:00:00Z","duration_ms":10,"evidence":{"screenshot_path":"run/first.png"}},
					{"index":1,"action":"click","status":"passed","started_at":"2026-10-26T12:00:01Z","duration_ms":20,"evidence":{"screenshot_path":"run/first.png"}},
					{"index":2,"action":"assert","status":"passed","started_at":"2026-10-26T12:00:02Z","duration_ms":30,"evidence":{"screenshot_path":""}},
					{"index":3,"action":"verify","status":"passed","started_at":"2026-10-26T12:00:03Z","duration_ms":40,"evidence":{"screenshot_path":"run/final.png"}}
				]
			}
		}`),
	}
	if err := piStore.AppendHarnessEvent(ctx, ref, events[0]); err != nil {
		t.Fatalf("append started event: %v", err)
	}
	unlinked := harEvent(
		piharness.EventCaseCompleted,
		run.ID,
		ref.ProviderAttemptID,
		2,
		`{"verdict":"passed","case":{},"dry_run":{"status":"passed","steps":[]}}`,
	)
	if err := piStore.AppendHarnessEvent(ctx, ref, unlinked); err == nil {
		t.Fatal("append case completion without a completed finish attempt = nil, want error")
	}
	for _, ev := range events[1:] {
		if err := piStore.AppendHarnessEvent(ctx, ref, ev); err != nil {
			t.Fatalf("append event %d: %v", ev.Sequence, err)
		}
	}

	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	var finishSpans []domain.Span
	for _, span := range details.Spans {
		if span.Kind == domain.SpanOperation {
			finishSpans = append(finishSpans, span)
		}
	}
	if len(finishSpans) != 1 {
		t.Fatalf("operation spans = %d, want exactly the finish tool span", len(finishSpans))
	}
	finishSpan := finishSpans[0]
	if finishSpan.Name != finishTool {
		t.Fatalf("finish span name = %q, want %q", finishSpan.Name, finishTool)
	}
	if len(details.Operations) != 1 {
		t.Fatalf("operation results = %d, want 1", len(details.Operations))
	}
	operation := details.Operations[0]
	if operation.Status != "passed" {
		t.Fatalf("operation status = %q, want passed", operation.Status)
	}
	if operation.SpanID != finishSpan.SpanID || operation.SpanAttemptID == "" {
		t.Fatalf(
			"operation links = span %q attempt %q, want finish span %q and a linked attempt",
			operation.SpanID, operation.SpanAttemptID, finishSpan.SpanID,
		)
	}

	var result map[string]any
	if err := json.Unmarshal(operation.Result, &result); err != nil {
		t.Fatalf("decode operation result: %v", err)
	}
	if !reflect.DeepEqual(result, map[string]any{
		"status":       "passed",
		"execution_id": "exec-1",
		"steps": []any{
			map[string]any{"index": float64(0), "action": "goto", "status": "passed", "started_at": "2026-10-26T12:00:00Z", "duration_ms": float64(10), "evidence": map[string]any{"screenshot_path": "run/first.png"}},
			map[string]any{"index": float64(1), "action": "click", "status": "passed", "started_at": "2026-10-26T12:00:01Z", "duration_ms": float64(20), "evidence": map[string]any{"screenshot_path": "run/first.png"}},
			map[string]any{"index": float64(2), "action": "assert", "status": "passed", "started_at": "2026-10-26T12:00:02Z", "duration_ms": float64(30), "evidence": map[string]any{"screenshot_path": ""}},
			map[string]any{"index": float64(3), "action": "verify", "status": "passed", "started_at": "2026-10-26T12:00:03Z", "duration_ms": float64(40), "evidence": map[string]any{"screenshot_path": "run/final.png"}},
		},
	}) {
		t.Fatalf("operation result = %s, want the dry_run object", operation.Result)
	}

	var artifacts map[string]any
	if err := json.Unmarshal(operation.Artifacts, &artifacts); err != nil {
		t.Fatalf("decode operation artifacts: %v", err)
	}
	wantArtifacts := map[string]any{
		"screenshot_paths": []any{"run/first.png", "run/final.png"},
	}
	if !reflect.DeepEqual(artifacts, wantArtifacts) {
		t.Fatalf("operation artifacts = %#v, want %#v", artifacts, wantArtifacts)
	}
}

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
	events[3].Payload = json.RawMessage(
		`{"tool_call_id":"tc1","tool_name":"mcp__browser_harness__finish_case"}`,
	)
	events[5].Payload = json.RawMessage(
		`{"tool_call_id":"tc1","tool_name":"mcp__browser_harness__finish_case","is_error":false}`,
	)
	events[6].Payload = json.RawMessage(`{
		"verdict":"passed",
		"case":{"case_version":"loop.case.v1","steps":[]},
		"dry_run":{"status":"passed","steps":[]}
	}`)
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
		RunID:             run.ID,
		ConversationID:    conversation.ID,
		Model:             event.ModelRef{Provider: "pi", Name: "browser-model"},
		Messages:          []domain.Message{{ID: "m", ConversationID: conversation.ID, Ordinal: 1, Role: "user", Content: "go"}},
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
