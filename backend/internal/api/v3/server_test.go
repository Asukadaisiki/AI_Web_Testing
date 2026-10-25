package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/agentprofile"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
)

func TestArtifactsServesConfiguredSharedVolumeFile(t *testing.T) {
	artifactDir := t.TempDir()
	artifactPath := filepath.Join(artifactDir, "run_1", "evidence.png")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("browser screenshot"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	handler := New(ServerConfig{
		Repository:  &fakeRepository{},
		Profile:     profile,
		ArtifactDir: artifactDir,
		Stream:      stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}),
		Live:        live.NewMemoryStore(),
	}).Handler()

	response := request(t, handler, http.MethodGet, "/artifacts/run_1/evidence.png", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Body.String() != "browser screenshot" {
		t.Fatalf("artifact = %q, want browser screenshot", response.Body.String())
	}
}

func TestSendMessageReturns202AndStableIDs(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{
		"content":"verify the cart",
		"agent_id":"browser-eval",
		"model":{"provider":"openai-compatible","name":"glm-5.3-flash"},
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
	if repository.message.AgentID != "browser-eval" {
		t.Fatalf("agent id = %q", repository.message.AgentID)
	}
	if repository.message.Model != (event.ModelRef{Provider: "openai-compatible", Name: "glm-5.3-flash"}) {
		t.Fatalf("stored model = %#v, want configured model", repository.message.Model)
	}
	if len(repository.message.Operations) != 1 || repository.message.Operations[0].Callback.URL == "" {
		t.Fatalf("run operations = %#v, want server-owned browser operation", repository.message.Operations)
	}
}

func TestSendMessageRejectsUnconfiguredModel(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodPost, "/api/v1/conversations/conv_1/messages", `{
		"content":"verify the cart",
		"agent_id":"browser-eval",
		"model":{"provider":"openai-compatible","name":"unapproved-model"},
		"idempotency_key":"request-unapproved-model"
	}`)
	assertErrorCode(t, response, http.StatusBadRequest, "model_not_available")
}

func TestListProjectsSerializesEmptyProjectsAsArray(t *testing.T) {
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodGet, "/api/v1/projects", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Projects []json.RawMessage `json:"projects"`
	}
	decodeJSON(t, response, &body)
	if body.Projects == nil {
		t.Fatalf("projects = null, want []: %s", response.Body.String())
	}
	if len(body.Projects) != 0 {
		t.Fatalf("projects = %#v, want []", body.Projects)
	}
}

func TestDeleteWorkspaceResourcesExposeIdempotentNoContentRoutes(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	for _, test := range []struct {
		path string
	}{
		{path: "/api/v1/projects/prj_1"},
		{path: "/api/v1/conversations/conv_1"},
	} {
		for attempt := 1; attempt <= 2; attempt++ {
			response := request(t, handler, http.MethodDelete, test.path, "")
			if response.Code != http.StatusNoContent {
				t.Fatalf("DELETE %s attempt %d status = %d, want %d: %s",
					test.path, attempt, response.Code, http.StatusNoContent, response.Body.String())
			}
			if response.Body.Len() != 0 {
				t.Fatalf("DELETE %s attempt %d body = %q, want empty",
					test.path, attempt, response.Body.String())
			}
		}
	}
	if len(repository.deletedProjectIDs) != 1 || repository.deletedProjectIDs["prj_1"] != 2 {
		t.Fatalf("deleted project IDs = %#v", repository.deletedProjectIDs)
	}
	if len(repository.deletedConversationIDs) != 1 || repository.deletedConversationIDs["conv_1"] != 2 {
		t.Fatalf("deleted conversation IDs = %#v", repository.deletedConversationIDs)
	}
}

func TestDeleteWorkspaceResourceReportsUnsafeStateAsConflict(t *testing.T) {
	repository := &fakeRepository{deleteErr: postgres.ErrConflict}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	for _, path := range []string{
		"/api/v1/projects/prj_active",
		"/api/v1/conversations/conv_active",
	} {
		response := request(t, handler, http.MethodDelete, path, "")
		assertErrorCode(t, response, http.StatusConflict, "conflict")
	}
}

func TestListConversationsSerializesEmptyConversationsAsArray(t *testing.T) {
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodGet, "/api/v1/projects/prj_empty/conversations", "")
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
		ID: "conv_empty", ProjectID: "prj_1", Title: "Empty", Metadata: json.RawMessage(`{}`),
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
			ID: "conv_1", ProjectID: "prj_1", Title: "History", Metadata: json.RawMessage(`{}`),
		},
		runs: []domain.Run{
			{
				ID:                "run_first",
				ProjectID:         "prj_1",
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
				ProjectID:         "prj_1",
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
		ProjectID:       "prj_1",
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
		RetryOfRunID: "run_failed",
		Status:       domain.RunQueued,
	}}
	handler := newTestServer(t, repository, stream.NewService(
		&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
	))

	response := request(t, handler, http.MethodPost, "/api/v1/runs/run_failed/reruns",
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
	if body.RunID != "run_new" || body.Status != domain.RunQueued || body.RetryOfRunID != "run_failed" {
		t.Fatalf("response = %#v", body)
	}
	if repository.rerunInput != (postgres.CreateRerunParams{
		RunID: "run_failed", IdempotencyKey: "rerun-api-1",
	}) {
		t.Fatalf("rerun input = %#v", repository.rerunInput)
	}
}

func TestRerunRejectsInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing idempotency key", body: `{}`},
		{name: "blank idempotency key", body: `{"idempotency_key":"  "}`},
		{name: "unknown field", body: `{"idempotency_key":"rerun-api-1","unexpected":true}`},
		{name: "multiple JSON values", body: `{"idempotency_key":"rerun-api-1"} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{}
			handler := newTestServer(t, repository, stream.NewService(
				&fakeEventReader{}, live.NewMemoryStore(), stream.Config{},
			))

			response := request(t, handler, http.MethodPost, "/api/v1/runs/run_failed/reruns", test.body)
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

			response := request(t, handler, http.MethodPost, "/api/v1/runs/run_failed/reruns",
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
	body := `{"content":"verify the cart","agent_id":"browser-eval","model":{"provider":"openai-compatible","name":"glm-5.3-flash"},"idempotency_key":"request-1"}`

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
	recorder := httptest.NewRecorder()

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
	handler := newTestServerWithLive(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}), failingLiveStore{})

	response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/cancel", `{}`)
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

	unknown := request(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"A","callback_url":"https://attacker.invalid"}`)
	assertErrorCode(t, unknown, http.StatusBadRequest, "invalid_request")

	oversized := request(t, handler, http.MethodPost, "/api/v1/projects", `{"name":"`+strings.Repeat("x", maxRequestBodyBytes)+`"}`)
	assertErrorCode(t, oversized, http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestAgentsDoNotExposeServerCallbackURLsOrCredentials(t *testing.T) {
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))
	response := request(t, handler, http.MethodGet, "/api/v1/agents", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"callback", "worker.internal", "secret"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("/agents leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestEventsEncodeEphemeralEventsWithoutID(t *testing.T) {
	liveStore := live.NewMemoryStore()
	if err := liveStore.Append(context.Background(), "run_1", live.LiveEvent{
		Type: "token.delta", Data: json.RawMessage(`{"delta":"hi"}`),
	}); err != nil {
		t.Fatalf("append live event: %v", err)
	}
	handler := newTestServer(t, &fakeRepository{}, stream.NewService(&fakeEventReader{}, liveStore, stream.Config{
		ReadBlock: time.Millisecond,
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_1/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(done)
	}()
	waitForBody(t, recorder, "event: token.delta")
	cancel()
	<-done
	if strings.Contains(recorder.Body.String(), "id:") {
		t.Fatalf("ephemeral SSE must not include an id: %q", recorder.Body.String())
	}
}

func TestEventsHeartbeatAndRequestCancellation(t *testing.T) {
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	handler := New(ServerConfig{
		Repository: &fakeRepository{},
		Profile:    profile,
		Stream: stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{
			ReadBlock: time.Hour,
		}),
		Live:              live.NewMemoryStore(),
		HeartbeatInterval: time.Millisecond,
	}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_1/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()
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

func TestResponsePersistsAndEnqueuesContinuation(t *testing.T) {
	repository := &fakeRepository{}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))
	response := request(t, handler, http.MethodPost, "/api/v1/runs/run_1/responses", `{"kind":"approval","content":"approved","idempotency_key":"response-1"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if repository.response.RunID != "run_1" || repository.response.Content != "approved" || !repository.response.Continuation {
		t.Fatalf("response contract = %#v", repository.response)
	}
}

func newTestServer(t *testing.T, repository *fakeRepository, service *stream.Service) http.Handler {
	t.Helper()
	return newTestServerWithLive(t, repository, service, live.NewMemoryStore())
}

func newTestServerWithLive(t *testing.T, repository *fakeRepository, service *stream.Service, liveStore live.LiveStore) http.Handler {
	t.Helper()
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	return New(ServerConfig{
		Repository: repository,
		Profile:    profile,
		Stream:     service,
		Live:       liveStore,
		Health: healthDependencies{
			Postgres: func(context.Context) error { return nil },
			Kafka:    func(context.Context) error { return nil },
			Redis:    func(context.Context) error { return nil },
			Worker:   func(context.Context) error { return nil },
		},
	}).Handler()
}

func newTestServerWithHealth(t *testing.T, repository *fakeRepository, health healthDependencies) http.Handler {
	t.Helper()
	profile, err := agentprofile.LoadBrowserEval("https://worker.internal")
	if err != nil {
		t.Fatalf("load browser profile: %v", err)
	}
	return New(ServerConfig{
		Repository: repository,
		Profile:    profile,
		Stream:     stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}),
		Live:       live.NewMemoryStore(),
		Health:     health,
	}).Handler()
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

func waitForBody(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recorder.Body.String(), want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("SSE body never contained %q: %q", want, recorder.Body.String())
}

type fakeRepository struct {
	message                postgres.CreateMessageParams
	cancelledRunID         string
	response               postgres.SubmitResponseParams
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

func (r *fakeRepository) SubmitResponse(_ context.Context, input postgres.SubmitResponseParams) error {
	r.response = input
	return nil
}

func (r *fakeRepository) PendingOutbox(context.Context) (int, error) {
	return r.pendingOutbox, nil
}

func (r *fakeRepository) ListProjects(context.Context) ([]postgres.Project, error) { return nil, nil }
func (r *fakeRepository) CreateProject(context.Context, postgres.CreateProjectParams) (postgres.Project, error) {
	return postgres.Project{}, nil
}
func (r *fakeRepository) DeleteProject(_ context.Context, projectID string) error {
	if r.deletedProjectIDs == nil {
		r.deletedProjectIDs = map[string]int{}
	}
	r.deletedProjectIDs[projectID]++
	return r.deleteErr
}
func (r *fakeRepository) ListConversations(context.Context, string) ([]postgres.Conversation, error) {
	return nil, nil
}
func (r *fakeRepository) CreateConversation(context.Context, postgres.CreateConversationParams) (postgres.Conversation, error) {
	return postgres.Conversation{}, nil
}
func (r *fakeRepository) DeleteConversation(_ context.Context, conversationID string) error {
	if r.deletedConversationIDs == nil {
		r.deletedConversationIDs = map[string]int{}
	}
	r.deletedConversationIDs[conversationID]++
	return r.deleteErr
}
func (r *fakeRepository) GetConversation(
	context.Context,
	string,
) (postgres.Conversation, []domain.Message, []domain.Run, error) {
	if r.conversation.ID != "" {
		return r.conversation, r.messages, r.runs, nil
	}
	return postgres.Conversation{}, nil, nil, postgres.ErrNotFound
}
func (r *fakeRepository) GetRun(context.Context, string) (domain.Run, error) {
	if r.run.ID != "" {
		return r.run, nil
	}
	return domain.Run{}, postgres.ErrNotFound
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
func (failingLiveStore) SetLease(context.Context, string, string, time.Duration) error {
	return errors.New("redis unavailable")
}
func (failingLiveStore) RequestCancel(context.Context, string, time.Duration) error {
	return errors.New("redis unavailable")
}
func (failingLiveStore) IsCancelRequested(context.Context, string) (bool, error) {
	return false, errors.New("redis unavailable")
}
