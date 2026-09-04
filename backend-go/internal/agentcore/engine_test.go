package agentcore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

type scriptedModel struct {
	responses []ModelResponse
	requests  [][]Message
}

func (m *scriptedModel) Complete(
	_ context.Context,
	messages []Message,
	_ []ToolDefinition,
) (ModelResponse, error) {
	m.requests = append(m.requests, append([]Message(nil), messages...))
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestEnginePausesAndResumesWithToolResult(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{
			ToolCalls: []ModelTool{{
				ID:   "call-1",
				Name: "ask_user_question",
				Arguments: `{
					"questions":[{
						"id":"entry_url",
						"question":"请输入测试地址",
						"type":"text",
						"required":true
					}]
				}`,
			}},
		},
		{Content: "信息完整，可以开始探索。"},
	}}
	repository := NewMemoryRepository()
	runService := NewService(repository)
	registry, err := tools.NewRegistry(AskUserTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := NewEngine(runService, model, registry, 4)

	run, err := engine.Start(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != RunStatusWaitingUser || run.PendingToolCallID == nil {
		t.Fatalf("run = %#v", run)
	}

	run, err = engine.Resume(
		context.Background(),
		run.ID,
		*run.PendingToolCallID,
		ResumeToolCallRequest{
			Answers:  map[string]any{"entry_url": "https://example.com/login"},
			NextStep: "continue",
		},
	)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if run.Status != RunStatusCompleted {
		t.Fatalf("status = %q, want %q", run.Status, RunStatusCompleted)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model request count = %d, want 2", len(model.requests))
	}

	secondRequest := model.requests[1]
	lastMessage := secondRequest[len(secondRequest)-1]
	if lastMessage.Role != "tool" || lastMessage.ToolCallID != "call-1" {
		t.Fatalf("resume message = %#v", lastMessage)
	}
	var resumePayload ResumeToolCallRequest
	if decodeErr := json.Unmarshal([]byte(lastMessage.Content), &resumePayload); decodeErr != nil {
		t.Fatalf("decode resume message: %v", decodeErr)
	}
	if resumePayload.Answers["entry_url"] != "https://example.com/login" {
		t.Fatalf("resume answers = %#v", resumePayload.Answers)
	}

	events, err := engine.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	wantTypes := []EventType{
		EventRunStarted,
		EventToolStarted,
		EventToolArgsDelta,
		EventToolPending,
		EventToolResult,
		EventToolFinished,
		EventMessageStarted,
		EventMessageDelta,
		EventMessageFinished,
		EventRunFinished,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(wantTypes))
	}
	for index, event := range events {
		if event.Type != wantTypes[index] {
			t.Fatalf("events[%d].Type = %q, want %q", index, event.Type, wantTypes[index])
		}
	}
}
