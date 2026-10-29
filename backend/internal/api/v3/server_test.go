package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
)

func TestSendMessageReturns202AndStableIDs(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{
		"content":"verify the cart",
		"runtime_id":"pi","harness_id":"version",
		"model":{"provider":"openai-compatible","name":"glm-5.3-flash","reasoning_effort":"high"},
		"idempotency_key":"request-1"
	}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	var body struct {
		MessageID string `json:"message_id"`
		RunID     string `json:"run_id"`
		Status    string `json:"status"`
	}
	decodeJSON(t, response, &body)
	if body != (struct {
		MessageID string `json:"message_id"`
		RunID     string `json:"run_id"`
		Status    string `json:"status"`
	}{MessageID: "msg_1", RunID: "run_1", Status: "queued"}) {
		t.Fatalf("response = %#v", body)
	}
	if repository.message.RuntimeID != "pi" || repository.message.HarnessVersionID != "version" {
		t.Fatalf("selectors = %+v", repository.message)
	}
	if repository.message.Model != (event.ModelRef{Provider: "openai-compatible", Name: "glm-5.3-flash", ReasoningEffort: "high"}) {
		t.Fatalf("stored model = %#v, want configured model", repository.message.Model)
	}
}

func TestSendMessageRejectsUnconfiguredModel(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{
		"content":"verify the cart",
		"runtime_id":"pi","harness_id":"version",
		"model":{"provider":"openai-compatible","name":"unapproved-model"},
		"idempotency_key":"request-unapproved-model"
	}`)
	assertErrorCode(t, response, http.StatusBadRequest, "model_not_available")
}

func TestListConversationsSerializesEmptyConversationsAsArray(t *testing.T) {
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodGet, "/api/v1/projects/project_1/conversations", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Conversations []json.RawMessage `json:"conversations"`
	}
	decodeJSON(t, response, &body)
	if body.Conversations == nil {
		t.Fatalf("conversations = null, want []: %s", response.Body.String())
	}
	if len(body.Conversations) != 0 {
		t.Fatalf("conversations = %#v, want []", body.Conversations)
	}
}

func TestGetConversationSerializesEmptyHistoryAsArrays(t *testing.T) {
	repository := &fakeRepository{conversation: postgres.Conversation{
		ID: "conv_empty", ProjectID: "project_1", Title: "Empty", Metadata: json.RawMessage("{}"),
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/conversations/conv_empty", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Messages []json.RawMessage `json:"messages"`
		Runs     []json.RawMessage `json:"runs"`
	}
	decodeJSON(t, response, &body)
	if body.Messages == nil {
		t.Fatalf("messages = null, want []: %s", response.Body.String())
	}
	if len(body.Messages) != 0 {
		t.Fatalf("messages = %#v, want []", body.Messages)
	}
	if body.Runs == nil {
		t.Fatalf("runs = null, want []: %s", response.Body.String())
	}
	if len(body.Runs) != 0 {
		t.Fatalf("runs = %#v, want []", body.Runs)
	}
}

func TestGetConversationReturnsPublicRunLineageWithoutSpecification(t *testing.T) {
	firstCreatedAt := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	firstStartedAt := firstCreatedAt.Add(time.Minute)
	firstFinishedAt := firstStartedAt.Add(2 * time.Minute)
	firstUpdatedAt := firstFinishedAt
	secondCreatedAt := firstCreatedAt.Add(5 * time.Minute)
	secondStartedAt := secondCreatedAt.Add(time.Minute)
	secondCancelRequestedAt := secondStartedAt.Add(time.Minute)
	secondFinishedAt := secondCancelRequestedAt.Add(time.Minute)
	secondUpdatedAt := secondFinishedAt
	repository := &fakeRepository{
		conversation: postgres.Conversation{
			ID: "conv_1", ProjectID: "project_1", Title: "History", Metadata: json.RawMessage("{}"),
		},
		runs: []domain.Run{
			{
				ID:                "run_first",
				ProjectID:         "project_1",
				ConversationID:    "conv_1",
				SourceMessageID:   "msg_first",
				RetryOfRunID:      "",
				Status:            domain.RunSucceeded,
				Specification:     json.RawMessage(`{"secret":"first-must-not-leak"}`),
				CheckpointVersion: 4,
				CancelRequestedAt: nil,
				StartedAt:         &firstStartedAt,
				FinishedAt:        &firstFinishedAt,
				CreatedAt:         firstCreatedAt,
				UpdatedAt:         firstUpdatedAt,
			},
			{
				ID:                "run_second",
				ProjectID:         "project_1",
				ConversationID:    "conv_1",
				SourceMessageID:   "msg_second",
				RetryOfRunID:      "run_first",
				Status:            domain.RunFailed,
				Specification:     json.RawMessage(`{"secret":"second-must-not-leak"}`),
				CheckpointVersion: 7,
				CancelRequestedAt: &secondCancelRequestedAt,
				StartedAt:         &secondStartedAt,
				FinishedAt:        &secondFinishedAt,
				CreatedAt:         secondCreatedAt,
				UpdatedAt:         secondUpdatedAt,
			},
		},
	}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodGet, "/api/v1/conversations/conv_1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Runs []struct {
			ID              string          `json:"id"`
			SourceMessageID string          `json:"source_message_id"`
			RetryOfRunID    string          `json:"retry_of_run_id"`
			Specification   json.RawMessage `json:"specification"`
		} `json:"runs"`
	}
	decodeJSON(t, response, &body)
	if len(body.Runs) != 2 {
		t.Fatalf("runs = %#v, want two runs", body.Runs)
	}
	if body.Runs[0].ID != "run_first" ||
		body.Runs[0].SourceMessageID != "msg_first" ||
		body.Runs[0].RetryOfRunID != "" {
		t.Fatalf("first run = %#v", body.Runs[0])
	}
	if body.Runs[1].ID != "run_second" ||
		body.Runs[1].SourceMessageID != "msg_second" ||
		body.Runs[1].RetryOfRunID != "run_first" {
		t.Fatalf("second run = %#v", body.Runs[1])
	}
	for index, run := range body.Runs {
		if run.Specification != nil {
			t.Fatalf("conversation run %d exposed specification: %s", index, run.Specification)
		}
	}
}

func TestGetRunReturnsPublicLineageWithoutSpecification(t *testing.T) {
	createdAt := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	finishedAt := startedAt.Add(2 * time.Minute)
	updatedAt := finishedAt
	repository := &fakeRepository{run: domain.Run{
		ID:              "run_1",
		ProjectID:       "project_1",
		ConversationID:  "conv_1",
		SourceMessageID: "msg_source",
		RetryOfRunID:    "run_original",
		Status:          domain.RunFailed,
		Specification: json.RawMessage(`{
			"command":{
				"operations":[{
					"callback":{"url":"https://worker.internal/private-callback"},
					"input_schema":{"secret":"must-not-leak"}
				}]
			}
		}`),
		CheckpointVersion: 8,
		CancelRequestedAt: nil,
		StartedAt:         &startedAt,
		FinishedAt:        &finishedAt,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var body struct {
		SourceMessageID string          `json:"source_message_id"`
		RetryOfRunID    string          `json:"retry_of_run_id"`
		Specification   json.RawMessage `json:"specification"`
	}
	decodeJSON(t, response, &body)
	if body.SourceMessageID != "msg_source" || body.RetryOfRunID != "run_original" {
		t.Fatalf("run lineage = %#v", body)
	}
	if body.Specification != nil {
		t.Fatalf("/runs response exposed specification: %s", body.Specification)
	}
	for _, forbidden := range []string{"specification", "callback", "worker.internal", "input_schema", "must-not-leak"} {
		if strings.Contains(strings.ToLower(response.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("/runs response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestRerunReturnsAcceptedRun(t *testing.T) {
	repository := &fakeRepository{rerunResult: domain.Run{
		ID:           "run_new",
		RetryOfRunID: "run_1",
		Status:       domain.RunQueued,
	}}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/reruns",
		`{"idempotency_key":"rerun-api-1"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusAccepted, response.Body.String())
	}
	var body struct {
		RunID        string           `json:"run_id"`
		Status       domain.RunStatus `json:"status"`
		RetryOfRunID string           `json:"retry_of_run_id"`
	}
	decodeJSON(t, response, &body)
	if body.RunID != "run_new" || body.Status != domain.RunQueued || body.RetryOfRunID != "run_1" {
		t.Fatalf("response = %#v", body)
	}
	if repository.rerunInput != (postgres.CreateRerunParams{
		RunID: "run_1", IdempotencyKey: "rerun-api-1",
	}) {
		t.Fatalf("rerun input = %#v", repository.rerunInput)
	}
}

func TestRerunRejectsInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing idempotency key", body: "{}"},
		{name: "blank idempotency key", body: `{"idempotency_key":"  "}`},
		{name: "unknown field", body: `{"idempotency_key":"rerun-api-1","unexpected":true}`},
		{name: "multiple JSON values", body: `{"idempotency_key":"rerun-api-1"} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			handler := newTestServer(t, repository, stream.NewService(
				&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
			))

			response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/reruns", test.body)
			assertErrorCode(t, response, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestRerunMapsRepositoryErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "conflict", err: postgres.ErrConflict, status: http.StatusConflict, code: "conflict"},
		{name: "not found", err: postgres.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "postgres unavailable", err: errors.New("database unavailable"), status: http.StatusServiceUnavailable, code: "postgres_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{rerunErr: test.err}
			handler := newTestServer(t, repository, stream.NewService(
				&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
			))

			response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/reruns",
				`{"idempotency_key":"rerun-api-1"}`)
			assertErrorCode(t, response, test.status, test.code)
		})
	}
}

func TestGetTraceExposesRedactedCanonicalDetails(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Spans: []domain.Span{{
			ID: "span-row-1", RunID: "run_1", TraceID: "trace_1", SpanID: "operation-1",
			Kind: domain.SpanOperation, Name: "browser_execute_case", Status: "failed",
			Input: json.RawMessage(`{"case":{"name":"checkout"}}`), StartedAt: now,
		}},
		Attempts: []postgres.TraceAttemptDetail{{
			ID: "attempt-1", SpanID: "operation-1", AttemptNumber: 1,
			OperationID: "operation-call-1", Status: "failed",
			Error:     json.RawMessage(`{"kind":"worker_error","message":"browser failed"}`),
			StartedAt: now,
		}},
		Usage: []postgres.TraceUsageDetail{{
			ID: "usage-1", SpanID: "operation-1", SpanAttemptID: "attempt-1",
			Provider: "openai-compatible", Model: "fixture",
			InputTokens: 12, OutputTokens: 8, TotalTokens: 20, CostMicros: 44,
			Details: json.RawMessage(`{"cached_tokens":2}`), CreatedAt: now,
		}},
		Operations: []postgres.TraceOperationDetail{{
			ID: "evidence-1", SpanID: "operation-1", SpanAttemptID: "attempt-1",
			OperationID: "operation-call-1", Status: "failed",
			Result: json.RawMessage(`{"steps":[{
				"index":0,
				"action":"goto",
				"status":"failed",
				"started_at":"2026-10-21T12:00:00Z",
				"duration_ms":500,
				"evidence":{"screenshot_path":"failure.png"}
			}]}`),
			Artifacts: json.RawMessage(`[{"kind":"screenshot","path":"failure.png"}]`),
			Error:     json.RawMessage(`{"kind":"worker_error","message":"browser failed"}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, required := range []string{
		`"attempts"`, `"attempt_number":1`, `"usage"`, `"total_tokens":20`,
		`"operation_evidence"`, `"artifacts"`, `"failure.png"`,
		`"derived_nodes"`, `"id":"derived:operation-1:step:0"`,
		`"parent_span_id":"operation-1"`, `"source_evidence_id":"evidence-1"`,
		`"kind":"step"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("trace response missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{`"request"`, `"response"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("trace response leaked attempt %s: %s", forbidden, body)
		}
	}
}

func TestGetTraceAttachesDecisionContextOnlyToModelSpans(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	modelInput := json.RawMessage(`{
		"messages":[
			{"role":"user","content":"Check checkout"},
			{"role":"tool","content":"login succeeded","token":"remove-observation-secret"}
		],
		"token":"remove-input-secret"
	}`)
	modelOutput := json.RawMessage(`{
		"message":{
			"content":"I will validate checkout.",
			"tool_calls":[{
				"name":"browser_execute_case",
				"arguments":{"case":"checkout","api_key":"remove-argument-secret"}
			}]
		},
		"secret":"remove-output-secret"
	}`)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Spans: []domain.Span{
			{
				ID: "model-row", RunID: "run_1", TraceID: "trace_1", SpanID: "model-1",
				Kind: domain.SpanModel, Name: "plan", Status: "succeeded",
				Input: modelInput, Output: modelOutput, StartedAt: now,
			},
			{
				ID: "report-row", RunID: "run_1", TraceID: "trace_1", SpanID: "report-1",
				Kind: domain.SpanReport, Name: "report", Status: "succeeded",
				Input:     json.RawMessage(`{"messages":[{"role":"user","content":"Write report"}]}`),
				Output:    json.RawMessage(`{"message":{"content":"Visible report"}}`),
				StartedAt: now,
			},
			{
				ID: "operation-row", RunID: "run_1", TraceID: "trace_1", SpanID: "operation-1",
				Kind: domain.SpanOperation, Name: "browser_execute_case", Status: "succeeded",
				Input: modelInput, Output: modelOutput, StartedAt: now,
			},
			{
				ID: "reasoning-row", RunID: "run_1", TraceID: "trace_1", SpanID: "reasoning-1",
				Kind: domain.SpanReasoning, Name: "reasoning", Status: "succeeded",
				Input: modelInput, Output: modelOutput, StartedAt: now,
			},
			{
				ID: "malformed-model-row", RunID: "run_1", TraceID: "trace_1", SpanID: "model-2",
				Kind: domain.SpanModel, Name: "model", Status: "failed",
				Input: json.RawMessage(`{"messages":[`), Output: json.RawMessage(`{"message":`),
				StartedAt: now,
			},
		},
	}}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Spans []json.RawMessage `json:"spans"`
	}
	decodeJSON(t, response, &body)
	if len(body.Spans) != 5 {
		t.Fatalf("span count = %d, want 5", len(body.Spans))
	}

	type projectedSpan struct {
		ID              string                 `json:"id"`
		Kind            domain.SpanKind        `json:"kind"`
		Input           json.RawMessage        `json:"input"`
		Output          json.RawMessage        `json:"output"`
		DecisionContext *publicDecisionContext `json:"decision_context"`
	}
	spans := make(map[string]projectedSpan, len(body.Spans))
	rawSpans := make(map[string]map[string]json.RawMessage, len(body.Spans))
	for _, raw := range body.Spans {
		var span projectedSpan
		if err := json.Unmarshal(raw, &span); err != nil {
			t.Fatalf("decode projected span: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("decode projected span fields: %v", err)
		}
		spans[span.ID] = span
		rawSpans[span.ID] = fields
	}

	wantModelContext := &publicDecisionContext{
		Objective: &publicDecisionMessage{Role: "user", Content: "Check checkout"},
		PriorObservation: &publicDecisionMessage{
			Role: "tool", Content: "login succeeded",
		},
		SelectedOperation: &publicSelectedOperation{
			Name: "browser_execute_case", Arguments: json.RawMessage(`{"case":"checkout"}`),
		},
		VisibleExplanation: "I will validate checkout.",
	}
	if got := spans["model-row"].DecisionContext; !reflect.DeepEqual(got, wantModelContext) {
		t.Fatalf("model decision context = %#v, want %#v", got, wantModelContext)
	}
	for _, id := range []string{"report-row", "operation-row", "reasoning-row", "malformed-model-row"} {
		if _, exists := rawSpans[id]["decision_context"]; exists {
			t.Fatalf("%s unexpectedly contains decision_context: %s", id, rawSpans[id]["decision_context"])
		}
	}

	if got, want := string(spans["model-row"].Input),
		`{"messages":[{"content":"Check checkout","role":"user"},{"content":"login succeeded","role":"tool"}]}`; got != want {
		t.Fatalf("canonical model input = %s, want %s", got, want)
	}
	if got, want := string(spans["model-row"].Output),
		`{"message":{"content":"I will validate checkout.","tool_calls":[{"arguments":{"case":"checkout"},"name":"browser_execute_case"}]}}`; got != want {
		t.Fatalf("canonical model output = %s, want %s", got, want)
	}
	if !strings.Contains(string(repository.trace.Spans[0].Input), "remove-input-secret") ||
		!strings.Contains(string(repository.trace.Spans[0].Output), "remove-output-secret") ||
		!strings.Contains(string(repository.trace.Spans[0].Output), "remove-argument-secret") {
		t.Fatalf("handler mutated canonical model payloads: input=%s output=%s",
			repository.trace.Spans[0].Input, repository.trace.Spans[0].Output)
	}
}

func TestGetTraceProjectsUnknownOperationAsSingleFallbackNode(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Spans: []domain.Span{{
			ID: "span-row-1", RunID: "run_1", TraceID: "trace_1", SpanID: "operation-1",
			Kind: domain.SpanOperation, Name: "custom_operation", Status: "failed",
			StartedAt: now,
		}},
		Operations: []postgres.TraceOperationDetail{{
			ID: "evidence-1", SpanID: "operation-1", OperationID: "operation-call-1",
			Status: "failed", Result: json.RawMessage(`{"message":"custom failure"}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		DerivedNodes []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"derived_nodes"`
	}
	decodeJSON(t, response, &body)
	if len(body.DerivedNodes) != 1 {
		t.Fatalf("derived node count = %d, want 1: %s", len(body.DerivedNodes), response.Body.String())
	}
	if node := body.DerivedNodes[0]; node.Kind != "unknown_tool" || node.Name != "unknown_tool" {
		t.Fatalf("derived node = %#v, want unknown_tool kind and name", node)
	}
}

func TestGetTraceRedactsCanonicalJSONAndPreservesRawDerivedNodes(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Spans: []domain.Span{{
			ID: "span-row-1", RunID: "run_1", TraceID: "trace_1", SpanID: "operation-1",
			Kind: domain.SpanOperation, Name: "browser_execute_case", Status: "failed",
			Input:     json.RawMessage(`{"case":{"name":"checkout","token":"span-token"}}`),
			Output:    json.RawMessage(`{"summary":"checkout failed","secret":"span-secret"}`),
			StartedAt: now,
		}},
		Attempts: []postgres.TraceAttemptDetail{{
			ID: "attempt-1", SpanID: "operation-1", AttemptNumber: 1,
			Status:    "failed",
			Error:     json.RawMessage(`{"message":"browser failed","nested":{"password":"attempt-password"}}`),
			StartedAt: now,
		}},
		Usage: []postgres.TraceUsageDetail{{
			ID: "usage-1", SpanID: "operation-1", InputTokens: 12, OutputTokens: 8,
			TotalTokens: 20, CostMicros: 44,
			Details:   json.RawMessage(`{"cached_tokens":2,"headers":{"authorization":"usage-authorization"}}`),
			CreatedAt: now,
		}},
		Operations: []postgres.TraceOperationDetail{{
			ID: "evidence-1", SpanID: "operation-1", OperationID: "operation-call-1",
			Status: "failed",
			Result: json.RawMessage(`{"steps":[{
				"index":0,
				"action":"goto",
				"status":"failed",
				"started_at":"2026-10-21T12:00:00Z",
				"duration_ms":500,
				"token":"result-token",
				"evidence":{"api_key":"step-api-key"}
			}],"report":{"summary":"visible report"}}`),
			Artifacts: json.RawMessage(`[{"kind":"screenshot","path":"failure.png","metadata":{"api_key":"artifact-api-key"}}]`),
			Error:     json.RawMessage(`{"kind":"worker_error","message":"visible failure","nested":{"cookie":"error-cookie"}}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Spans             []json.RawMessage `json:"spans"`
		Attempts          []json.RawMessage `json:"attempts"`
		Usage             []json.RawMessage `json:"usage"`
		OperationEvidence []struct {
			Result    map[string]any `json:"result"`
			Artifacts []any          `json:"artifacts"`
			Error     map[string]any `json:"error"`
		} `json:"operation_evidence"`
		DerivedNodes []struct {
			Data     map[string]any `json:"data"`
			Evidence map[string]any `json:"evidence"`
		} `json:"derived_nodes"`
	}
	decodeJSON(t, response, &body)

	canonical, err := json.Marshal(struct {
		Spans             []json.RawMessage `json:"spans"`
		Attempts          []json.RawMessage `json:"attempts"`
		Usage             []json.RawMessage `json:"usage"`
		OperationEvidence any               `json:"operation_evidence"`
	}{
		Spans: body.Spans, Attempts: body.Attempts, Usage: body.Usage,
		OperationEvidence: body.OperationEvidence,
	})
	if err != nil {
		t.Fatalf("marshal canonical trace arrays: %v", err)
	}
	for _, forbidden := range []string{
		`"authorization"`, `"cookie"`, `"password"`, `"token"`, `"api_key"`, `"secret"`,
		"span-token", "span-secret", "attempt-password", "usage-authorization",
		"result-token", "step-api-key", "artifact-api-key", "error-cookie",
	} {
		if strings.Contains(strings.ToLower(string(canonical)), strings.ToLower(forbidden)) {
			t.Fatalf("canonical trace arrays leaked %q: %s", forbidden, canonical)
		}
	}
	if len(body.OperationEvidence) != 1 {
		t.Fatalf("operation evidence count = %d, want 1", len(body.OperationEvidence))
	}
	canonicalSteps, ok := body.OperationEvidence[0].Result["steps"].([]any)
	if !ok || len(canonicalSteps) != 1 {
		t.Fatalf("canonical steps = %#v, want one step", body.OperationEvidence[0].Result["steps"])
	}
	canonicalStep, ok := canonicalSteps[0].(map[string]any)
	if !ok {
		t.Fatalf("canonical step = %#v, want object", canonicalSteps[0])
	}
	if _, exists := canonicalStep["token"]; exists {
		t.Fatalf("canonical operation result retained sensitive token: %#v", canonicalStep)
	}
	if len(body.DerivedNodes) != 1 {
		t.Fatalf("derived node count = %d, want 1", len(body.DerivedNodes))
	}
	if got := body.DerivedNodes[0].Data["token"]; got != "result-token" {
		t.Fatalf("derived data token = %#v, want persisted value", got)
	}
	if got := body.DerivedNodes[0].Evidence["api_key"]; got != "step-api-key" {
		t.Fatalf("derived evidence api_key = %#v, want persisted value", got)
	}
	for _, required := range []string{
		`"name":"checkout"`, `"summary":"checkout failed"`, `"cached_tokens":2`,
		`"status":"failed"`, `"summary":"visible report"`, `"kind":"screenshot"`,
		`"path":"failure.png"`, `"message":"visible failure"`,
	} {
		if !strings.Contains(string(canonical), required) {
			t.Fatalf("canonical trace arrays omitted usable field %q: %s", required, canonical)
		}
	}
	if !strings.Contains(string(repository.trace.Operations[0].Result), `"token":"result-token"`) {
		t.Fatalf("handler mutated canonical operation result: %s", repository.trace.Operations[0].Result)
	}
}

func TestGetTracePreservesLargeIntegersWhileRedactingSensitiveSiblings(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Operations: []postgres.TraceOperationDetail{{
			ID: "evidence-1", OperationID: "operation-call-1", Status: "failed",
			Result:    json.RawMessage(`{"nested":{"sequence":9007199254740993,"token":"result-token"}}`),
			Artifacts: json.RawMessage(`[{"metadata":{"size":9007199254740995,"api_key":"artifact-api-key"}}]`),
			Error:     json.RawMessage(`{"nested":{"code":9007199254740997,"password":"error-password"}}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	body := response.Body.String()
	for _, required := range []string{
		`"sequence":9007199254740993`,
		`"size":9007199254740995`,
		`"code":9007199254740997`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("trace response changed exact integer %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{
		`"token"`, `"api_key"`, `"password"`,
		"result-token", "artifact-api-key", "error-password",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("trace response leaked %q: %s", forbidden, body)
		}
	}
}

func TestSendMessageDuplicateIdempotencyKeyReturnsSameRun(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))
	body := `{"content":"verify the cart","runtime_id":"pi","harness_id":"version","model":{"provider":"openai-compatible","name":"glm-5.3-flash","reasoning_effort":"high"},"idempotency_key":"request-1"}`

	first := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", body)
	second := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("idempotency response differs:\nfirst=%s\nsecond=%s", first.Body.String(), second.Body.String())
	}
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	reader := &fakeEventReader{events: []stream.DurableEvent{
		{Sequence: 1, Type: "run.started", Data: json.RawMessage(`{"status":"running"}`)},
		{Sequence: 2, Type: "span.completed", Data: json.RawMessage(`{"span":"model"}`)},
	}}
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(reader, live.NewMemoryStore(), stream.Config{
		ReadBlock: time.Millisecond,
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_1/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	recorder := &synchronizedRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(done)
	}()
	waitForBody(t, recorder, "id: 2")
	cancel()
	<-done

	body := recorder.Body.String()
	if strings.Contains(body, "id: 1") || !strings.Contains(body, "id: 2") {
		t.Fatalf("SSE body = %q", body)
	}
}

func TestCancelPersistsWhenRedisFails(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, failingLiveStore{}, stream.Config{}))

	response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/cancel", "{}")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if repository.cancelledRunID != "run_1" {
		t.Fatalf("durable cancel = %q", repository.cancelledRunID)
	}
}

func TestHealthReturns503OnlyWhenPostgresIsDown(t *testing.T) {
	handler := newTestServerWithHealth(t, &fakeRepository{}, healthDependencies{
		Postgres: func(context.Context) error { return errors.New("postgres unavailable") },
		Kafka:    func(context.Context) error { return errors.New("kafka unavailable") },
		Redis:    func(context.Context) error { return errors.New("redis unavailable") },
		Worker:   func(context.Context) error { return errors.New("worker unavailable") },
	})

	response := request(t, handler, http.MethodGet, "/api/v1/health", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", response.Code, response.Body.String())
	}
	var body healthResponse
	decodeJSON(t, response, &body)
	if body.Status != "down" || body.Dependencies["postgres"].Status != "down" {
		t.Fatalf("health = %#v", body)
	}
}

func TestHealthReportsKafkaAndRedisSeparately(t *testing.T) {
	repository := &fakeRepository{pendingOutbox: 3}
	handler := newTestServerWithHealth(t, repository, healthDependencies{
		Postgres: func(context.Context) error { return nil },
		Kafka:    func(context.Context) error { return errors.New("kafka unavailable") },
		Redis:    func(context.Context) error { return errors.New("redis unavailable") },
		Worker:   func(context.Context) error { return errors.New("worker unavailable") },
		Langfuse: func(context.Context) error { return errors.New("langfuse unavailable") },
	})

	response := request(t, handler, http.MethodGet, "/api/v1/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var body healthResponse
	decodeJSON(t, response, &body)
	if body.Status != "degraded" ||
		body.Dependencies["kafka"].Status != "down" ||
		body.Dependencies["kafka"].PendingOutbox != 3 ||
		body.Dependencies["redis"].Mode != "postgres_poll" ||
		body.Dependencies["langfuse"].Status != "down" {
		t.Fatalf("health = %#v", body)
	}
}

func TestHealthReportsUnprobedLangfuseAsDisabled(t *testing.T) {
	handler := newTestServerWithHealth(t, &fakeRepository{}, healthDependencies{
		Postgres: func(context.Context) error { return nil },
	})

	response := request(t, handler, http.MethodGet, "/api/v1/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var body healthResponse
	decodeJSON(t, response, &body)
	if body.Status != "ok" || body.Dependencies["langfuse"].Status != "disabled" {
		t.Fatalf("health = %#v", body)
	}
}

func TestHealthPreservesZeroPendingOutbox(t *testing.T) {
	handler := newTestServerWithHealth(t, &fakeRepository{pendingOutbox: 0}, healthDependencies{
		Postgres: func(context.Context) error { return nil },
		Kafka:    func(context.Context) error { return nil },
	})

	response := request(t, handler, http.MethodGet, "/api/v1/health", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var body map[string]json.RawMessage
	decodeJSON(t, response, &body)
	var dependencies map[string]map[string]json.RawMessage
	if err := json.Unmarshal(body["dependencies"], &dependencies); err != nil {
		t.Fatalf("decode dependencies: %v", err)
	}
	if _, ok := dependencies["kafka"]["pending_outbox"]; !ok {
		t.Fatalf("health response omitted zero pending_outbox: %s", response.Body.String())
	}
}

func TestStrictJSONRejectsUnknownFieldsAndOversizedBodies(t *testing.T) {
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	unknown := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{"content":"A","callback_url":"https://attacker.invalid"}`)
	assertErrorCode(t, unknown, http.StatusBadRequest, "invalid_request")

	oversized := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{"content":"`+strings.Repeat("x", maxRequestBodyBytes)+`"}`)
	assertErrorCode(t, oversized, http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestEventsEmitOnlyPostgreSQLPayloadsAndSequences(t *testing.T) {
	liveStore := live.NewMemoryStore()
	if err := liveStore.Append(context.Background(), "run_1", live.LiveEvent{
		Type: "token.delta", Data: json.RawMessage(`{"delta":"hi"}`),
	}); err != nil {
		t.Fatalf("append live event: %v", err)
	}
	reader := &fakeEventReader{events: []stream.DurableEvent{{
		Sequence: 7, Type: "run.started", Data: json.RawMessage(`{"status":"running"}`),
	}}}
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(reader, liveStore, stream.Config{
		ReadBlock: time.Millisecond,
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_1/events", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	body := recorder.Body.String()
	if !strings.Contains(body, "id: 7\n") || !strings.Contains(body, "event: run.started\n") ||
		!strings.Contains(body, `data: {"status":"running"}`) {
		t.Fatalf("durable SSE missing: %q", body)
	}
	if strings.Contains(body, "token.delta") || strings.Contains(body, `"delta"`) ||
		strings.Contains(body, "run.events.available") {
		t.Fatalf("Redis payload escaped into SSE: %q", body)
	}
}

func TestEventsHeartbeatAndRequestCancellation(t *testing.T) {

	handler := authenticatedTestHandler(New(ServerConfig{
		Repository:  &fakeRepository{},
		BridgeToken: testBridgeToken,
		Stream: stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{
			ReadBlock: time.Hour,
		}),
		HeartbeatInterval: time.Millisecond,
	}).Handler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_1/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	recorder := &synchronizedRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(done)
	}()
	waitForBody(t, recorder, ": heartbeat")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not exit after request cancellation")
	}
}

func newTestServer(t *testing.T, repository *fakeRepository, service *stream.Service) http.Handler {
	t.Helper()

	return authenticatedTestHandler(New(ServerConfig{
		Repository:  repository,
		BridgeToken: testBridgeToken,
		Runtimes: []execution.Runtime{{ID: "pi", Available: true, Model: execution.Model{
			Provider: "openai-compatible", Name: "glm-5.3-flash", ReasoningEffort: "high",
		}}},

		Stream: service,
		Health: healthDependencies{
			Postgres: func(context.Context) error { return nil },
			Kafka:    func(context.Context) error { return nil },
			Redis:    func(context.Context) error { return nil },
			Worker:   func(context.Context) error { return nil },
		},
	}).Handler())
}

func newTestServerWithHealth(t *testing.T, repository *fakeRepository, health healthDependencies) http.Handler {
	t.Helper()

	return authenticatedTestHandler(New(ServerConfig{
		Repository:  repository,
		BridgeToken: testBridgeToken,

		Stream: stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}),
		Health: health,
	}).Handler())
}

func authenticatedTestHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setTestIdentity(r)
		next.ServeHTTP(w, r)
	})
}

func setTestIdentity(r *http.Request) {
	r.Header.Set("Authorization", "Bearer "+testBridgeToken)
	r.Header.Set("X-Agenttrace-Project-Id", "project_1")
	r.Header.Set("X-Langfuse-Project-Id", "lf_1")
	r.Header.Set("X-Agenttrace-Role", "member")
	r.Header.Set("X-Agenttrace-Actor-Id", "actor_1")
}

func request(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(target); err != nil {
		t.Fatalf("decode JSON response %q: %v", recorder.Body.String(), err)
	}
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeJSON(t, recorder, &body)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
}

type synchronizedRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *synchronizedRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(p)
}

func (r *synchronizedRecorder) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ResponseRecorder.Flush()
}

func (r *synchronizedRecorder) bodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

func waitForBody(t *testing.T, recorder *synchronizedRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recorder.bodyString(), want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("SSE body never contained %q: %q", want, recorder.bodyString())
}

type fakeRepository struct {
	message                postgres.CreateMessageParams
	cancelledRunID         string
	pendingOutbox          int
	conversation           postgres.Conversation
	messages               []domain.Message
	runs                   []domain.Run
	run                    domain.Run
	rerunInput             postgres.CreateRerunParams
	rerunResult            domain.Run
	rerunErr               error
	trace                  postgres.TraceDetails
	deleteErr              error
	deletedProjectIDs      map[string]int
	deletedConversationIDs map[string]int
}

func (r *fakeRepository) CreateMessageAndRun(_ context.Context, input postgres.CreateMessageParams) (domain.Message, domain.Run, error) {
	r.message = input
	return domain.Message{ID: "msg_1", RunID: "run_1"}, domain.Run{ID: "run_1", Status: domain.RunQueued}, nil
}

func (r *fakeRepository) RequestCancel(_ context.Context, runID string) error {
	r.cancelledRunID = runID
	return nil
}

func (r *fakeRepository) PendingOutbox(context.Context) (int, error) {
	return r.pendingOutbox, nil
}

func (r *fakeRepository) ListConversations(context.Context, string) ([]postgres.Conversation, error) {
	return nil, nil
}
func (r *fakeRepository) CreateConversation(context.Context, postgres.CreateConversationParams) (postgres.Conversation, error) {
	return postgres.Conversation{}, nil
}

func (r *fakeRepository) GetConversation(
	context.Context,
	string,
) (postgres.Conversation, []domain.Message, []domain.Run, error) {
	if r.conversation.ID != "" {
		return r.conversation, r.messages, r.runs, nil
	}
	return postgres.Conversation{ID: "conv_1", ProjectID: "project_1"}, r.messages, r.runs, nil
}
func (r *fakeRepository) GetRun(context.Context, string) (domain.Run, error) {
	if r.run.ID != "" {
		return r.run, nil
	}
	return domain.Run{ID: "run_1", ProjectID: "project_1", ConversationID: "conv_1"}, nil
}

func (*fakeRepository) GetProjectBinding(context.Context, string) (postgres.ProjectBinding, error) {
	return postgres.ProjectBinding{AgenttraceProjectID: "project_1", LangfuseProjectID: "lf_1", Status: "active"}, nil
}
func (r *fakeRepository) CreateRerun(_ context.Context, input postgres.CreateRerunParams) (domain.Run, error) {
	r.rerunInput = input
	return r.rerunResult, r.rerunErr
}
func (r *fakeRepository) ListTrace(context.Context, string) ([]domain.Span, error) { return nil, nil }
func (r *fakeRepository) GetTraceDetails(context.Context, string) (postgres.TraceDetails, error) {
	return r.trace, nil
}

type fakeEventReader struct{ events []stream.DurableEvent }

func (r *fakeEventReader) ReadAfter(_ context.Context, _ string, after int64) ([]stream.DurableEvent, error) {
	var result []stream.DurableEvent
	for _, event := range r.events {
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, nil
}

type failingLiveStore struct{}

func (failingLiveStore) Append(context.Context, string, live.LiveEvent) error {
	return errors.New("redis unavailable")
}
func (failingLiveStore) Read(context.Context, string, string, time.Duration) ([]live.LiveEvent, error) {
	return nil, errors.New("redis unavailable")
}
