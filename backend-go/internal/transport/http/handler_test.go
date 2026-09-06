package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/harness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type staticModel struct{}

type blockingModel struct {
	started chan struct{}
}

type subscribeFirstAPI struct {
	AgentAPI
	subscribed bool
	runID      string
	event      agentservice.Event
}

func (api *subscribeFirstAPI) Subscribe(runID string) agentservice.Subscription {
	api.subscribed = true
	api.runID = runID
	wake := make(chan struct{})
	return agentservice.Subscription{Wake: wake, Cancel: func() { close(wake) }}
}

func (api *subscribeFirstAPI) ListOwnedEvents(
	_ context.Context,
	runID string,
	_ int64,
	afterSeq int64,
) ([]agentservice.Event, error) {
	if !api.subscribed {
		return nil, errors.New("history queried before subscription")
	}
	if runID != api.runID || api.event.Seq <= afterSeq {
		return nil, nil
	}
	return []agentservice.Event{api.event}, nil
}

func (api *subscribeFirstAPI) GetOwnedRun(
	context.Context,
	string,
	int64,
) (agentservice.AgentRun, error) {
	return agentservice.AgentRun{Status: agentservice.RunStatusCompleted}, nil
}

func (staticModel) Complete(
	_ context.Context,
	_ []agent.Message,
	_ []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	return agent.ModelResponse{Content: "已收到测试需求。"}, nil
}

func (m blockingModel) Complete(
	ctx context.Context,
	_ []agent.Message,
	_ []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	close(m.started)
	<-ctx.Done()
	return agent.ModelResponse{}, ctx.Err()
}

func newTestServer(t *testing.T) AgentAPI {
	t.Helper()
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	registry, err := tools.NewRegistry(tools.AskUserTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return harness.New(runService, staticModel{}, registry, 4)
}

type staticPlanningStore struct{}

func (staticPlanningStore) CreateSession(
	context.Context,
	int64,
	planning.CreateSessionRequest,
) (planning.SessionDetail, error) {
	return planning.SessionDetail{}, nil
}
func (staticPlanningStore) ListSessions(context.Context, int64) ([]planning.SessionSummary, error) {
	return nil, nil
}
func (staticPlanningStore) GetSession(context.Context, int64, int64) (planning.SessionDetail, error) {
	return planning.SessionDetail{}, nil
}
func (staticPlanningStore) UpdateSession(
	context.Context,
	int64,
	int64,
	planning.UpdateSessionRequest,
) (planning.SessionDetail, error) {
	return planning.SessionDetail{}, nil
}
func (staticPlanningStore) DeleteSession(context.Context, int64, int64) error {
	return nil
}
func (staticPlanningStore) ListProjects(context.Context, int64, int64) ([]planning.ProjectSummary, error) {
	return nil, nil
}
func (staticPlanningStore) LinkProject(context.Context, int64, int64, int64) (planning.ProjectSummary, error) {
	return planning.ProjectSummary{}, nil
}
func (staticPlanningStore) UnlinkProject(context.Context, int64, int64, int64) error {
	return nil
}
func (staticPlanningStore) CreateProject(
	context.Context,
	int64,
	int64,
	planning.CreateProjectRequest,
) (planning.ProjectSummary, error) {
	return planning.ProjectSummary{}, nil
}
func (staticPlanningStore) ResolveRunContext(
	_ context.Context,
	actorUserID int64,
	sessionID int64,
) (string, int64, error) {
	if actorUserID != 1 || sessionID != 1 {
		return "", 0, planning.ErrSessionNotFound
	}
	return "1", 42, nil
}

func newHTTPTestServer(t *testing.T) *server.Hertz {
	t.Helper()
	return NewServer(
		"127.0.0.1:0",
		newTestServer(t),
		1,
		staticPlanningStore{},
		nil,
		nil,
		nil,
		nil,
	)
}

func TestHealth(t *testing.T) {
	server := newHTTPTestServer(t)
	response := ut.PerformRequest(server.Engine, "GET", "/health", nil).Result()

	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode(), consts.StatusOK)
	}
	if string(response.Body()) != `{"status":"ok"}` {
		t.Fatalf("body = %s", response.Body())
	}
}

func TestAuthRoutesAreNotExposed(t *testing.T) {
	server := newHTTPTestServer(t)
	routes := []struct {
		method string
		path   string
	}{
		{method: "POST", path: "/api/v2/auth/login"},
		{method: "GET", path: "/api/v2/auth/me"},
		{method: "POST", path: "/api/v2/auth/logout"},
	}
	for _, route := range routes {
		response := ut.PerformRequest(server.Engine, route.method, route.path, nil).Result()
		if response.StatusCode() != consts.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", route.method, route.path, response.StatusCode())
		}
	}
}

func TestStartRunAndReplayEvents(t *testing.T) {
	server := newHTTPTestServer(t)
	body := []byte(`{"conversation_id":"1","project_id":999,"message":"测试登录"}`)
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/agent/runs",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if response.StatusCode() != consts.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.StatusCode(), response.Body())
	}

	var run agentservice.AgentRun
	if err := json.Unmarshal(response.Body(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.ID == "" || run.ProjectID != 42 || run.Status != agentservice.RunStatusRunning {
		t.Fatalf("run = %#v", run)
	}

	for range 100 {
		runResponse := ut.PerformRequest(
			server.Engine,
			"GET",
			"/api/v2/agent/runs/"+run.ID,
			nil,
		).Result()
		if err := json.Unmarshal(runResponse.Body(), &run); err != nil {
			t.Fatalf("decode current run: %v", err)
		}
		if run.Status == agentservice.RunStatusCompleted {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if run.Status != agentservice.RunStatusCompleted {
		t.Fatalf("run did not complete: %#v", run)
	}

	eventsResponse := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/agent/runs/"+run.ID+"/events?after_seq=0",
		nil,
	).Result()
	if eventsResponse.StatusCode() != consts.StatusOK {
		t.Fatalf("events status = %d, body = %s", eventsResponse.StatusCode(), eventsResponse.Body())
	}

	var payload struct {
		Events []agentservice.Event `json:"events"`
	}
	if err := json.Unmarshal(eventsResponse.Body(), &payload); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(payload.Events) != 5 || payload.Events[0].Type != agentservice.EventRunStarted {
		t.Fatalf("events = %#v", payload.Events)
	}
	if payload.Events[4].Type != agentservice.EventRunFinished {
		t.Fatalf("last event = %#v", payload.Events[4])
	}

}

func TestStartRunRejectsEmptyInput(t *testing.T) {
	server := newHTTPTestServer(t)
	body := []byte(`{"session_id":1,"message":""}`)
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/agent/runs",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()

	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.StatusCode(), consts.StatusBadRequest, response.Body())
	}
}

func TestCancelRunChecksOwnershipAndIsIdempotent(t *testing.T) {
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	registry, err := tools.NewRegistry(tools.AskUserTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	started := make(chan struct{})
	engine := harness.New(runService, blockingModel{started: started}, registry, 2)
	server := NewServer(
		"127.0.0.1:0",
		engine,
		1,
		staticPlanningStore{},
		nil,
		nil,
		nil,
		nil,
	)
	run, err := engine.StartOwnedProjectAsync(
		context.Background(),
		1,
		"conversation-1",
		42,
		"block",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectAsync() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run did not start")
	}

	body := []byte(`{"reason":"driver timeout"}`)
	for attempt := 0; attempt < 2; attempt++ {
		response := ut.PerformRequest(
			server.Engine,
			"POST",
			"/api/v2/agent/runs/"+run.ID+"/cancel",
			&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
			ut.Header{Key: "Content-Type", Value: "application/json"},
		).Result()
		if response.StatusCode() != consts.StatusOK {
			t.Fatalf("attempt %d status = %d, body = %s", attempt, response.StatusCode(), response.Body())
		}
		var cancelled agentservice.AgentRun
		if err := json.Unmarshal(response.Body(), &cancelled); err != nil {
			t.Fatalf("decode cancelled run: %v", err)
		}
		if cancelled.Status != agentservice.RunStatusCancelled {
			t.Fatalf("cancelled run = %#v", cancelled)
		}
	}
	events, err := runService.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	cancelEvents := 0
	for _, event := range events {
		if event.Type == agentservice.EventRunCancelled {
			cancelEvents++
			if event.Payload["reason"] != "driver timeout" {
				t.Fatalf("cancel reason = %#v", event.Payload["reason"])
			}
		}
	}
	if cancelEvents != 1 {
		t.Fatalf("cancel event count = %d, want 1", cancelEvents)
	}

	foreign, err := runService.StartOwnedProjectRun(
		context.Background(),
		2,
		"conversation-2",
		42,
		"foreign",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectRun() error = %v", err)
	}
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/agent/runs/"+foreign.ID+"/cancel",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if response.StatusCode() != consts.StatusForbidden {
		t.Fatalf("foreign status = %d, body = %s", response.StatusCode(), response.Body())
	}
	foreign, err = runService.GetRun(context.Background(), foreign.ID)
	if err != nil || foreign.Status != agentservice.RunStatusRunning {
		t.Fatalf("foreign run = %#v, error = %v", foreign, err)
	}
}

func TestPlanningRoutesDoNotRequireAuthentication(t *testing.T) {
	server := newHTTPTestServer(t)
	response := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/planning/sessions",
		nil,
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode(), response.Body())
	}
}

func TestPlanningRoutesUseV2Contract(t *testing.T) {
	server := newHTTPTestServer(t)
	response := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/planning/sessions",
		nil,
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("v2 status = %d, body = %s", response.StatusCode(), response.Body())
	}

	legacyResponse := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v1/ai-planning/sessions",
		nil,
	).Result()
	if legacyResponse.StatusCode() != consts.StatusNotFound {
		t.Fatalf(
			"legacy status = %d, want %d",
			legacyResponse.StatusCode(),
			consts.StatusNotFound,
		)
	}
}

func TestEncodeSSEEvent(t *testing.T) {
	event := agentservice.Event{
		Seq:   7,
		Type:  agentservice.EventToolPending,
		RunID: "run-1",
		Payload: map[string]any{
			"tool": "ask_user_question",
		},
	}
	id, eventType, data, err := encodeSSEEvent(event)
	if err != nil {
		t.Fatalf("encodeSSEEvent() error = %v", err)
	}
	if id != "7" || eventType != "tool.pending" {
		t.Fatalf("id = %q, eventType = %q", id, eventType)
	}
	var decoded agentservice.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if decoded.RunID != "run-1" {
		t.Fatalf("decoded event = %#v", decoded)
	}
	rest, err := marshalEventList([]agentservice.Event{event})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rest, data) {
		t.Fatalf("REST bytes do not contain shared MarshalEvent bytes:\n%s\n%s", rest, data)
	}
}

func TestResearchLLMCallRESTAndSSEAssociationContract(t *testing.T) {
	tests := []struct {
		name       string
		toolCallID string
		payload    map[string]any
	}{
		{
			name:       "single",
			toolCallID: "tool-1",
			payload: map[string]any{
				"schema_version":   agentservice.ResearchLLMCallSchemaV1,
				"tool_call_status": agentservice.ToolCallAvailable,
				"tool_call_ids":    []string{"tool-1"},
			},
		},
		{
			name: "multiple",
			payload: map[string]any{
				"schema_version":   agentservice.ResearchLLMCallSchemaV1,
				"tool_call_status": agentservice.ToolCallAvailable,
				"tool_call_ids":    []string{"tool-1", "tool-2"},
			},
		},
		{
			name: "unavailable",
			payload: map[string]any{
				"schema_version":               agentservice.ResearchLLMCallSchemaV1,
				"tool_call_status":             agentservice.ToolCallUnavailable,
				"tool_call_unavailable_reason": agentservice.ToolCallUnavailableModelReturnedFinalText,
			},
		},
		{
			name: "legacy",
			payload: map[string]any{
				"schema_version": agentservice.ResearchLLMCallSchemaV1,
			},
		},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := agentservice.Event{
				Seq: int64(index + 1), Type: agentservice.EventResearchLLMCall,
				RunID: "run-1", StepID: "step-1", ToolCallID: testCase.toolCallID,
				Timestamp: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC),
				Payload:   testCase.payload,
			}
			_, eventType, sseData, err := encodeSSEEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if eventType != string(agentservice.EventResearchLLMCall) {
				t.Fatalf("event type = %q", eventType)
			}
			restData, err := marshalEventList([]agentservice.Event{event})
			if err != nil {
				t.Fatal(err)
			}
			var rest struct {
				Events []json.RawMessage `json:"events"`
			}
			if err := json.Unmarshal(restData, &rest); err != nil {
				t.Fatal(err)
			}
			if len(rest.Events) != 1 || !bytes.Equal(rest.Events[0], sseData) {
				t.Fatalf("REST/SSE mismatch:\n%s\n%s", restData, sseData)
			}
		})
	}
}

func TestStreamSubscribesBeforeHistoryReplay(t *testing.T) {
	api := &subscribeFirstAPI{event: agentservice.Event{
		Seq: 1, Type: agentservice.EventRunFinished, RunID: "run-1",
		ConversationID: "conversation-1", Timestamp: time.Now().UTC(),
		Payload: map[string]any{},
	}}
	subscription, events, err := subscribeAndList(
		context.Background(), api, "run-1", 1, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	if !api.subscribed || len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("subscribed = %v, events = %#v", api.subscribed, events)
	}
}
