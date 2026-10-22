package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestGetRunDoesNotExposeInternalSpecification(t *testing.T) {
	repository := &fakeRepository{run: domain.Run{
		ID:             "run_1",
		ProjectID:      "prj_1",
		ConversationID: "conv_1",
		Status:         domain.RunRunning,
		Specification: json.RawMessage(`{
			"command":{
				"operations":[{
					"callback":{"url":"https://worker.internal/private-callback"},
					"input_schema":{"secret":"must-not-leak"}
				}]
			}
		}`),
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"specification", "callback", "worker.internal", "input_schema", "must-not-leak"} {
		if strings.Contains(strings.ToLower(response.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("/runs response leaked %q: %s", forbidden, response.Body.String())
		}
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
			Result:    json.RawMessage(`{"steps":[{"status":"failed"}]}`),
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

func TestGetTraceRedactsNestedSensitiveCanonicalJSON(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{trace: postgres.TraceDetails{
		Spans: []domain.Span{{
			ID: "span-row-1", RunID: "run_1", TraceID: "trace_1", SpanID: "operation-1",
			Kind: domain.SpanOperation, Name: "browser_execute_case", Status: "failed",
			Input: json.RawMessage(`{"case":{"name":"checkout","token":"span-token"}}`),
			Output: json.RawMessage(`{"summary":"checkout failed","secret":"span-secret"}`),
			StartedAt: now,
		}},
		Attempts: []postgres.TraceAttemptDetail{{
			ID: "attempt-1", SpanID: "operation-1", AttemptNumber: 1,
			Status: "failed",
			Error: json.RawMessage(`{"message":"browser failed","nested":{"password":"attempt-password"}}`),
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
			Result: json.RawMessage(`{"steps":[{"status":"failed","token":"result-token"}],"report":{"summary":"visible report"}}`),
			Artifacts: json.RawMessage(`[{"kind":"screenshot","path":"failure.png","metadata":{"api_key":"artifact-api-key"}}]`),
			Error: json.RawMessage(`{"kind":"worker_error","message":"visible failure","nested":{"cookie":"error-cookie"}}`),
			CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := newTestServer(t, repository, stream.NewService(&fakeEventReader{}, live.NewMemoryStore(), stream.Config{}))

	response := request(t, handler, http.MethodGet, "/api/v1/runs/run_1/trace", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{
		`"authorization"`, `"cookie"`, `"password"`, `"token"`, `"api_key"`, `"secret"`,
		"span-token", "span-secret", "attempt-password", "usage-authorization",
		"result-token", "artifact-api-key", "error-cookie",
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("trace response leaked %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{
		`"name":"checkout"`, `"summary":"checkout failed"`, `"cached_tokens":2`,
		`"status":"failed"`, `"summary":"visible report"`, `"kind":"screenshot"`,
		`"path":"failure.png"`, `"message":"visible failure"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("trace response omitted usable canonical field %q: %s", required, body)
		}
	}
	if string(repository.trace.Operations[0].Result) != `{"steps":[{"status":"failed","token":"result-token"}],"report":{"summary":"visible report"}}` {
		t.Fatalf("handler mutated canonical operation result: %s", repository.trace.Operations[0].Result)
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
		body.Dependencies["redis"].Mode != "postgres_poll" {
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
	message        postgres.CreateMessageParams
	cancelledRunID string
	response       postgres.SubmitResponseParams
	pendingOutbox  int
	run            domain.Run
	trace          postgres.TraceDetails
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
func (r *fakeRepository) ListConversations(context.Context, string) ([]postgres.Conversation, error) {
	return nil, nil
}
func (r *fakeRepository) CreateConversation(context.Context, postgres.CreateConversationParams) (postgres.Conversation, error) {
	return postgres.Conversation{}, nil
}
func (r *fakeRepository) GetConversation(context.Context, string) (postgres.Conversation, []domain.Message, error) {
	return postgres.Conversation{}, nil, postgres.ErrNotFound
}
func (r *fakeRepository) GetRun(context.Context, string) (domain.Run, error) {
	if r.run.ID != "" {
		return r.run, nil
	}
	return domain.Run{}, postgres.ErrNotFound
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
