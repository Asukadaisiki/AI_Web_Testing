package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestGenericToolCompletionDoesNotCreateBrowserEvidence(t *testing.T) {
	ref := HarnessAttemptRef{RunID: "run-1", ID: "attempt-1"}
	ev := harEvent(piharness.EventToolCompleted, ref.RunID, "provider-1", 2,
		`{"tool_call_id":"call-1","tool_name":"lookup_record","is_error":false,"structured_result":{"value":42}}`)
	record, err := (PostgresPiTraceStore{}).deriveEvent(context.Background(), ref, ev)
	if err != nil {
		t.Fatal(err)
	}
	if record.Span == nil || record.Span.Name != "lookup_record" || record.Operation != nil {
		t.Fatalf("generic tool projection = %#v", record)
	}
}

func TestUsageProjectionPreservesUnknownAndReportedZeroCost(t *testing.T) {
	for _, test := range []struct {
		raw      string
		wantCost bool
	}{
		{`{"input_tokens":10,"output_tokens":5}`, false},
		{`{"input_tokens":0,"output_tokens":0,"cost_micros":0}`, true},
	} {
		record, err := modelUsage(json.RawMessage(test.raw), "attempt-1")
		if err != nil {
			t.Fatal(err)
		}
		var details map[string]any
		if err := json.Unmarshal(record.Details, &details); err != nil {
			t.Fatal(err)
		}
		_, hasCost := details["cost_micros"]
		if hasCost != test.wantCost {
			t.Fatalf("cost presence = %v in %s", hasCost, record.Details)
		}
	}
	if record, err := modelUsage(json.RawMessage(`{}`), "attempt-1"); err != nil || record != nil {
		t.Fatalf("unknown usage = %#v, %v", record, err)
	}
	for _, raw := range []string{`{"input_tokens":-1,"output_tokens":2}`, `{"input_tokens":1}`, `{"input_tokens":1,"output_tokens":2,"cost_micros":-1}`} {
		if _, err := modelUsage(json.RawMessage(raw), "attempt-1"); err == nil {
			t.Fatalf("accepted invalid usage %s", raw)
		}
	}
}

func TestUsageProjectionPreservesReportedTotalIncludingCachedTokens(t *testing.T) {
	record, err := modelUsage(json.RawMessage(`{"input_tokens":10,"output_tokens":5,"total_tokens":115,"cache_read_tokens":100}`), "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Usage.TotalTokens != 115 || !strings.Contains(string(record.Details), `"cache_read_tokens":100`) {
		t.Fatalf("reported usage total/cache lost: %#v", record)
	}
}

func TestProviderAttemptIdentityIsFreshForEachSession(t *testing.T) {
	seen := map[string]bool{}
	for range 16 {
		id := newProviderAttemptID("same-run")
		if id == "" || seen[id] {
			t.Fatalf("execution reused provider session identity %q", id)
		}
		seen[id] = true
	}
}

func TestModelSpanUsesReportedCallTimingAndInput(t *testing.T) {
	ref := HarnessAttemptRef{RunID: "run-1", ID: "attempt-1"}
	ev := harEvent(piharness.EventMessageCompleted, ref.RunID, "provider-1", 2,
		`{"text":"answer","provider":"openai-compatible","started_at":"2026-10-28T12:00:00Z","ended_at":"2026-10-28T12:00:02Z","input":{"messages":[{"role":"user","content":"actual request"}]},"model":"real-model"}`)
	span, attempt := (PostgresPiTraceStore{}).modelSpan(ref, ev)
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	if !span.StartedAt.Equal(start) || span.EndedAt == nil || !span.EndedAt.Equal(end) {
		t.Fatalf("model timing = %v / %v, want reported call interval", span.StartedAt, span.EndedAt)
	}
	if string(span.Input) != `{"messages":[{"role":"user","content":"actual request"}]}` ||
		string(attempt.Request) != string(span.Input) {
		t.Fatalf("model request = %s / %s", span.Input, attempt.Request)
	}
	if attempt.Provider != "openai-compatible" || !strings.Contains(string(attempt.Response), `"model":"real-model"`) {
		t.Fatalf("model attempt identity lost: %#v", attempt)
	}
}

func TestModelSpanMissingTimingAndInputStayUnknown(t *testing.T) {
	ref := HarnessAttemptRef{RunID: "run-1", ID: "attempt-1"}
	ev := harEvent(piharness.EventMessageCompleted, ref.RunID, "provider-1", 2, `{"text":"answer"}`)
	span, attempt := (PostgresPiTraceStore{}).modelSpan(ref, ev)
	if span.EndedAt != nil || attempt.EndedAt != nil {
		t.Fatal("missing model timing fabricated a zero-duration call")
	}
	if len(span.Input) != 0 || len(attempt.Request) != 0 {
		t.Fatal("missing model input fabricated an empty request")
	}
}

func TestModelProjectionRejectsInvalidReportedTiming(t *testing.T) {
	ref := HarnessAttemptRef{RunID: "run-1", ID: "attempt-1"}
	for _, raw := range []string{
		`{"text":"answer","started_at":"invalid"}`,
		`{"text":"answer","started_at":"2026-10-28T12:00:02Z","ended_at":"2026-10-28T12:00:00Z"}`,
	} {
		ev := harEvent(piharness.EventMessageCompleted, ref.RunID, "provider-1", 2, raw)
		if _, err := (PostgresPiTraceStore{}).deriveEvent(context.Background(), ref, ev); err == nil {
			t.Fatalf("accepted invalid model timing %s", raw)
		}
	}
}

func TestModelEvidencePersistsAndUsageCannotAttachToPriorHarnessAttempt(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "model evidence"}))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID, Content: "actual goal",
		Model: event.ModelRef{Provider: "pi", Name: "model"}, IdempotencyKey: t.Name(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatal(err)
	}
	if err := piStore.AppendHarnessEvent(ctx, ref, harEvent(piharness.EventMessageCompleted, run.ID, ref.ProviderAttemptID, 1,
		`{"text":"answer","input":{"messages":[{"role":"user","content":"actual goal"}]},"started_at":"2026-10-28T12:00:00Z","ended_at":"2026-10-28T12:00:02Z"}`)); err != nil {
		t.Fatal(err)
	}
	if err := piStore.AppendHarnessEvent(ctx, ref, harEvent(piharness.EventUsage, run.ID, ref.ProviderAttemptID, 2,
		`{"input_tokens":10,"output_tokens":5,"cost_micros":0}`)); err != nil {
		t.Fatal(err)
	}
	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var model *domain.Span
	for i := range details.Spans {
		if details.Spans[i].Kind == domain.SpanModel {
			model = &details.Spans[i]
		}
	}
	if model == nil || model.EndedAt == nil || model.EndedAt.Sub(model.StartedAt) != 2*time.Second ||
		!strings.Contains(string(model.Input), "actual goal") {
		t.Fatalf("durable model evidence = %#v", model)
	}
	if len(details.Usage) != 1 || details.Usage[0].InputTokens != 10 ||
		!strings.Contains(string(details.Usage[0].Details), `"cost_micros": 0`) {
		t.Fatalf("durable usage = %#v", details.Usage)
	}
	if _, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command)); !errors.Is(err, postgres.ErrCapacityBusy) {
		t.Fatalf("active redelivery must defer instead of allocating another attempt: %v", err)
	}
}

func TestPostgresPiTraceStorePersistsFinishCaseEvidence(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi finish evidence"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi finish evidence",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "finish the browser case",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-finish-evidence",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
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

func TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi failed tool evidence"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi failed tool evidence",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "assert missing text",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-failed-tool-evidence",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	const assertTextTool = "mcp__browser_harness__assert_text"
	events := []piharness.Event{
		harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`),
		harEvent(piharness.EventToolStarted, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"assert-1","tool_name":"`+assertTextTool+`"}`),
		harEvent(piharness.EventToolCompleted, run.ID, ref.ProviderAttemptID, 3, `{
			"tool_call_id":"assert-1",
			"tool_name":"`+assertTextTool+`",
			"is_error":false,
			"structured_result":{
				"ok":false,
				"error":"condition_unmet",
				"detail":"the text \"DoesNotExist\" is not present",
				"page":{"screenshot_path":"`+run.ID+`/obs_failed.png"},
				"steps":[{"index":0,"action":"goto"}]
			}
		}`),
	}
	for _, ev := range events {
		if err := piStore.AppendHarnessEvent(ctx, ref, ev); err != nil {
			t.Fatalf("append event %d: %v", ev.Sequence, err)
		}
	}

	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	if len(details.Operations) != 1 {
		t.Fatalf("operation results = %d, want 1", len(details.Operations))
	}
	operation := details.Operations[0]
	if operation.Status != "failed" {
		t.Fatalf("operation status = %q, want failed", operation.Status)
	}
	if operation.SpanAttemptID == "" {
		t.Fatal("operation has no linked span attempt")
	}
	if !strings.Contains(string(operation.Result), `"condition_unmet"`) {
		t.Fatalf("operation result = %s, want structured failure", operation.Result)
	}
	if !strings.Contains(string(operation.Error), `"condition_unmet"`) ||
		!strings.Contains(string(operation.Error), `DoesNotExist`) {
		t.Fatalf("operation error = %s, want stable error and detail", operation.Error)
	}
	var artifacts map[string]any
	if err := json.Unmarshal(operation.Artifacts, &artifacts); err != nil {
		t.Fatalf("decode operation artifacts: %v", err)
	}
	if !reflect.DeepEqual(artifacts, map[string]any{
		"screenshot_paths": []any{run.ID + "/obs_failed.png"},
	}) {
		t.Fatalf("operation artifacts = %#v", artifacts)
	}
}

func TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi finish deferral"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi finish deferral",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "finish case",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-finish-deferral",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	for _, ev := range []piharness.Event{
		harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`),
		harEvent(piharness.EventToolStarted, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"finish-1","tool_name":"mcp__browser_harness__finish_case"}`),
		harEvent(piharness.EventToolCompleted, run.ID, ref.ProviderAttemptID, 3,
			`{"tool_call_id":"finish-1","tool_name":"mcp__browser_harness__finish_case","is_error":false,"structured_result":{"ok":false,"error":"dry_run_failed"}}`),
	} {
		if err := piStore.AppendHarnessEvent(ctx, ref, ev); err != nil {
			t.Fatalf("append event %d: %v", ev.Sequence, err)
		}
	}
	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	if len(details.Operations) != 0 {
		t.Fatalf("finish completion operation results = %d, want 0 before browser.case.completed", len(details.Operations))
	}
}

func TestPostgresPiTraceStorePersistsFullEventStream(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi trace"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi trace",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "inspect and report",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model", ReasoningEffort: "high"},
		IdempotencyKey: "pi-trace-full",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
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

	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
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
	events[4].Payload = json.RawMessage(
		`{"tool_call_id":"tc1","observation":{"url":"http://worker:8100"}}`,
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

	status, err := piStore.CompleteHarnessRun(ctx, ref, PiRunOutcome{
		Status:    domain.RunSucceeded,
		Assistant: "final answer",
	})
	if err != nil {
		t.Fatalf("complete run: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("returned status = %q, want succeeded", status)
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

func TestPostgresPiTraceStoreAssociatesObservationsWithExactToolCall(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi observation association"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi observation association",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "interleave browser tools",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-observation-association",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	const (
		clickTool = "mcp__browser_harness__click"
		inputTool = "mcp__browser_harness__input"
	)
	for _, ev := range []piharness.Event{
		harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`),
		harEvent(piharness.EventToolStarted, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"call-click","tool_name":"`+clickTool+`"}`),
		harEvent(piharness.EventToolStarted, run.ID, ref.ProviderAttemptID, 3,
			`{"tool_call_id":"call-input","tool_name":"`+inputTool+`"}`),
		harEvent(piharness.EventObservation, run.ID, ref.ProviderAttemptID, 4,
			`{"tool_call_id":"call-click","observation":{"url":"http://worker/click"}}`),
		harEvent(piharness.EventToolCompleted, run.ID, ref.ProviderAttemptID, 5,
			`{"tool_call_id":"call-click","tool_name":"`+clickTool+`","is_error":false,"structured_result":{"ok":true}}`),
		harEvent(piharness.EventToolCompleted, run.ID, ref.ProviderAttemptID, 6,
			`{"tool_call_id":"call-input","tool_name":"`+inputTool+`","is_error":false,"structured_result":{"ok":true}}`),
	} {
		if err := piStore.AppendHarnessEvent(ctx, ref, ev); err != nil {
			t.Fatalf("append event %d: %v", ev.Sequence, err)
		}
	}

	details, err := store.GetTraceDetails(ctx, run.ID)
	if err != nil {
		t.Fatalf("get trace details: %v", err)
	}
	clickSpanID := stableID("span", run.ID, ref.ID, "tool", "call-click")
	var observation *domain.Span
	for i := range details.Spans {
		if details.Spans[i].Name == "observation" {
			observation = &details.Spans[i]
			break
		}
	}
	if observation == nil {
		t.Fatal("browser observation span was not persisted")
	}
	if observation.ParentSpanID != clickSpanID {
		t.Fatalf(
			"observation parent span = %q, want exact click span %q",
			observation.ParentSpanID, clickSpanID,
		)
	}
	if len(details.Operations) != 2 {
		t.Fatalf("operation results = %d, want 2", len(details.Operations))
	}
	operationSpans := map[string]string{}
	for _, operation := range details.Operations {
		operationSpans[operation.OperationID] = operation.SpanID
	}
	if operationSpans[stableID("operation", run.ID, ref.ID, "call-click")] != clickSpanID {
		t.Fatalf("click operation links = %#v, want click span %q", operationSpans, clickSpanID)
	}
}

func TestPostgresPiTraceStoreRejectsUnknownAndCrossAttemptObservationToolCalls(t *testing.T) {
	ctx, pool, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi invalid observation association"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi invalid observation association",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "reject invalid observation association",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-invalid-observation-association",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := piStore.AppendHarnessEvent(
		ctx, ref, harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`),
	); err != nil {
		t.Fatalf("append started: %v", err)
	}
	if err := piStore.AppendHarnessEvent(
		ctx, ref, harEvent(piharness.EventObservation, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"unknown","observation":{"url":"http://worker/unknown"}}`),
	); err == nil {
		t.Fatal("append unknown observation tool call = nil, want error")
	}

	// A foreign historical attempt is deliberately injected to test association
	// validation; the production claim API now forbids allocating a second one.
	foreign := postgres.HarnessAttempt{
		ID: "hat_foreign", RunID: run.ID, AttemptNumber: 2,
		ProviderAttemptID: "foreign-provider-attempt",
	}
	if _, err := pool.Exec(ctx, `INSERT INTO harness_attempts
		(id, run_id, attempt_number, provider_attempt_id, status, started_at)
		VALUES ($1, $2, $3, $4, 'active', now())`,
		foreign.ID, foreign.RunID, foreign.AttemptNumber, foreign.ProviderAttemptID); err != nil {
		t.Fatalf("inject foreign historical attempt: %v", err)
	}
	foreignRef := HarnessAttemptRef{
		ID:                foreign.ID,
		RunID:             foreign.RunID,
		AttemptNumber:     foreign.AttemptNumber,
		ProviderAttemptID: foreign.ProviderAttemptID,
	}
	if err := piStore.AppendHarnessEvent(
		ctx,
		foreignRef,
		harEvent(piharness.EventToolStarted, run.ID, foreignRef.ProviderAttemptID, 1,
			`{"tool_call_id":"foreign","tool_name":"mcp__browser_harness__click"}`),
	); !errors.Is(err, postgres.ErrStaleFence) {
		t.Fatalf("unfenced foreign attempt wrote evidence: %v", err)
	}
	if err := piStore.AppendHarnessEvent(
		ctx, ref, harEvent(piharness.EventObservation, run.ID, ref.ProviderAttemptID, 2,
			`{"tool_call_id":"foreign","observation":{"url":"http://worker/foreign"}}`),
	); err == nil {
		t.Fatal("append cross-attempt observation tool call = nil, want error")
	}
}

func TestPostgresPiTraceStoreStartRunUsesCommandCheckpointVersion(t *testing.T) {
	ctx, pool, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi checkpoint"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi checkpoint",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "checkpoint test",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-checkpoint-match",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := managedFixtureCommand(t, ctx, piStore, commandFor(run.ID))
	forged := command
	forged.CheckpointVersion = 5
	if _, err := piStore.StartRun(ctx, forged); !errors.Is(err, postgres.ErrConflict) {
		t.Fatalf("altered command checkpoint = %v, want ErrConflict", err)
	}
	ref, err := piStore.StartRun(ctx, command)
	if err != nil {
		t.Fatalf("start run with matching checkpoint = %v, want nil", err)
	}
	if _, err := piStore.CompleteHarnessRun(ctx, ref, PiRunOutcome{Status: domain.RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := piStore.ReleaseRunLease(ctx, ref, true); err != nil {
		t.Fatal(err)
	}

	// A second run with a stale checkpoint must conflict rather than silently
	// create the root span against an unexpected checkpoint.
	_, run2, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "checkpoint mismatch",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-checkpoint-mismatch",
	}))
	if err != nil {
		t.Fatalf("create run 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET checkpoint_version = 7 WHERE id = $1`, run2.ID); err != nil {
		t.Fatalf("advance run 2 checkpoint: %v", err)
	}
	command2 := managedFixtureCommand(t, ctx, piStore, commandFor(run2.ID))
	_, err = piStore.StartRun(ctx, command2)
	if !errors.Is(err, postgres.ErrConflict) {
		t.Fatalf("start run with mismatched checkpoint = %v, want ErrConflict", err)
	}
}

func TestPostgresPiTraceStoreDurableCancellationOverridesSuccessfulOutcome(t *testing.T) {
	ctx, _, store := newRuntimePostgresStore(t)
	project, err := store.CreateProject(ctx, managedProjectParams(postgres.CreateProjectParams{Name: "Pi cancel"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conversation, err := store.CreateConversation(ctx, postgres.CreateConversationParams{
		ProjectID: project.ID, Title: "Pi cancel",
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, run, err := store.CreateMessageAndRun(ctx, managedMessageParams(t, ctx, store, postgres.CreateMessageParams{
		ConversationID: conversation.ID,
		Content:        "cancel me",

		Model:          event.ModelRef{Provider: "pi", Name: "browser-model"},
		IdempotencyKey: "pi-cancel",
	}))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	piStore := PostgresPiTraceStore{Store: store, RuntimeWorkerID: "runtime"}
	command := commandFor(run.ID)
	command.ConversationID = conversation.ID
	ref, err := piStore.StartRun(ctx, managedFixtureCommand(t, ctx, piStore, command))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := piStore.AppendHarnessEvent(ctx, ref, harEvent(piharness.EventStarted, run.ID, ref.ProviderAttemptID, 1, `{}`)); err != nil {
		t.Fatalf("append started: %v", err)
	}
	if err := store.RequestCancel(ctx, run.ID); err != nil {
		t.Fatalf("request durable cancellation: %v", err)
	}
	status, err := piStore.CompleteHarnessRun(ctx, ref, PiRunOutcome{
		Status: domain.RunSucceeded,
	})
	if err != nil {
		t.Fatalf("complete cancelled: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("returned status = %q, want cancelled", status)
	}
	finished, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if finished.Status != domain.RunCancelled {
		t.Fatalf("run status = %q, want cancelled", finished.Status)
	}
}
