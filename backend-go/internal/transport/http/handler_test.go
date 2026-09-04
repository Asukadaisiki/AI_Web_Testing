package httptransport

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentcore"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestHealth(t *testing.T) {
	server := NewServer("127.0.0.1:0", agentcore.NewService(agentcore.NewMemoryRepository()))
	response := ut.PerformRequest(server.Engine, "GET", "/health", nil).Result()

	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode(), consts.StatusOK)
	}
	if string(response.Body()) != `{"status":"ok"}` {
		t.Fatalf("body = %s", response.Body())
	}
}

func TestStartRunAndReplayEvents(t *testing.T) {
	server := NewServer("127.0.0.1:0", agentcore.NewService(agentcore.NewMemoryRepository()))
	body := []byte(`{"conversation_id":"conversation-1","message":"测试登录"}`)
	response := ut.PerformRequest(
		server.Engine,
		"POST",
		"/api/v2/agent/runs",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	).Result()
	if response.StatusCode() != consts.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.StatusCode(), response.Body())
	}

	var run agentcore.AgentRun
	if err := json.Unmarshal(response.Body(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.ID == "" || run.Status != agentcore.RunStatusRunning {
		t.Fatalf("run = %#v", run)
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
		Events []agentcore.Event `json:"events"`
	}
	if err := json.Unmarshal(eventsResponse.Body(), &payload); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(payload.Events) != 1 || payload.Events[0].Type != agentcore.EventRunStarted {
		t.Fatalf("events = %#v", payload.Events)
	}
}

func TestStartRunRejectsEmptyInput(t *testing.T) {
	server := NewServer("127.0.0.1:0", agentcore.NewService(agentcore.NewMemoryRepository()))
	body := []byte(`{"conversation_id":"conversation-1","message":""}`)
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
