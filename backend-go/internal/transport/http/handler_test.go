package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/harness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type staticModel struct{}

func (staticModel) Complete(
	_ context.Context,
	_ []agent.Message,
	_ []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	return agent.ModelResponse{Content: "已收到测试需求。"}, nil
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

type staticAuthenticator struct{}

func (staticAuthenticator) Login(
	_ context.Context,
	email string,
	_ string,
) (authn.Identity, string, error) {
	return authn.Identity{UserID: 1, Email: email, DisplayName: "Test"}, "signed", nil
}

func (staticAuthenticator) CookieName() string {
	return "session"
}

func (staticAuthenticator) MaxAgeSeconds() int {
	return 43200
}

func (staticAuthenticator) Authenticate(
	_ context.Context,
	cookieHeader string,
) (authn.Identity, error) {
	switch cookieHeader {
	case "session=user-1":
		return authn.Identity{UserID: 1, Cookie: cookieHeader}, nil
	case "session=user-2":
		return authn.Identity{UserID: 2, Cookie: cookieHeader}, nil
	default:
		return authn.Identity{}, authn.ErrUnauthenticated
	}
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
		staticAuthenticator{},
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

func TestAuthRoutes(t *testing.T) {
	server := newHTTPTestServer(t)
	payload := `{"email":"owner@example.com","password":"secret"}`
	login := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/auth/login",
		&ut.Body{Body: bytes.NewBufferString(payload), Len: len(payload)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if login.StatusCode() != consts.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.StatusCode(), login.Body())
	}
	if len(login.Header.Peek("Set-Cookie")) == 0 {
		t.Fatal("login response has no Set-Cookie header")
	}

	me := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/auth/me",
		nil,
		ut.Header{Key: "Cookie", Value: "session=user-1"},
	).Result()
	if me.StatusCode() != consts.StatusOK {
		t.Fatalf("me status = %d, body = %s", me.StatusCode(), me.Body())
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
		ut.Header{Key: "Cookie", Value: "session=user-1"},
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
			ut.Header{Key: "Cookie", Value: "session=user-1"},
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
		ut.Header{Key: "Cookie", Value: "session=user-1"},
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

	foreignResponse := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/agent/runs/"+run.ID,
		nil,
		ut.Header{Key: "Cookie", Value: "session=user-2"},
	).Result()
	if foreignResponse.StatusCode() != consts.StatusForbidden {
		t.Fatalf(
			"foreign status = %d, want %d; body = %s",
			foreignResponse.StatusCode(),
			consts.StatusForbidden,
			foreignResponse.Body(),
		)
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
		ut.Header{Key: "Cookie", Value: "session=user-1"},
	).Result()

	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.StatusCode(), consts.StatusBadRequest, response.Body())
	}
}

func TestRunRoutesRequireAuthentication(t *testing.T) {
	server := newHTTPTestServer(t)
	response := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v2/agent/runs/run-1",
		nil,
	).Result()
	if response.StatusCode() != consts.StatusUnauthorized {
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
		ut.Header{Key: "Cookie", Value: "session=user-1"},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("v2 status = %d, body = %s", response.StatusCode(), response.Body())
	}

	legacyResponse := ut.PerformRequest(
		server.Engine,
		"GET",
		"/api/v1/ai-planning/sessions",
		nil,
		ut.Header{Key: "Cookie", Value: "session=user-1"},
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
}
