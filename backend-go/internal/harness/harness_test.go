package harness

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

type scriptedModel struct {
	responses []agent.ModelResponse
	requests  [][]agent.Message
}

type failingTool struct{}

func (failingTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "failing_tool",
		Description: "Always fails.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func (failingTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{}, errors.New("expected tool failure")
}

func (m *scriptedModel) Complete(
	_ context.Context,
	messages []agent.Message,
	_ []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	m.requests = append(m.requests, append([]agent.Message(nil), messages...))
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestEnginePausesAndResumesWithToolResult(t *testing.T) {
	model := &scriptedModel{responses: []agent.ModelResponse{
		{
			ToolCalls: []agent.ModelTool{{
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
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	registry, err := tools.NewRegistry(tools.AskUserTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := New(runService, model, registry, 4)

	run, err := engine.Start(context.Background(), "conversation-1", "测试登录")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != agentservice.RunStatusWaitingUser || run.PendingToolCallID == nil {
		t.Fatalf("run = %#v", run)
	}

	run, err = engine.Resume(
		context.Background(),
		run.ID,
		*run.PendingToolCallID,
		agentservice.ResumeToolCallRequest{
			Answers:  map[string]any{"entry_url": "https://example.com/login"},
			NextStep: "continue",
		},
	)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if run.Status != agentservice.RunStatusCompleted {
		t.Fatalf("status = %q, want %q", run.Status, agentservice.RunStatusCompleted)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model request count = %d, want 2", len(model.requests))
	}

	secondRequest := model.requests[1]
	lastMessage := secondRequest[len(secondRequest)-1]
	if lastMessage.Role != "tool" || lastMessage.ToolCallID != "call-1" {
		t.Fatalf("resume message = %#v", lastMessage)
	}
	var resumePayload agentservice.ResumeToolCallRequest
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
	wantTypes := []agentservice.EventType{
		agentservice.EventRunStarted,
		agentservice.EventToolStarted,
		agentservice.EventToolArgsDelta,
		agentservice.EventToolPending,
		agentservice.EventToolResult,
		agentservice.EventToolFinished,
		agentservice.EventMessageStarted,
		agentservice.EventMessageDelta,
		agentservice.EventMessageFinished,
		agentservice.EventRunFinished,
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

func TestEngineReturnsToolFailureToModelForRecovery(t *testing.T) {
	model := &scriptedModel{responses: []agent.ModelResponse{
		{
			ToolCalls: []agent.ModelTool{{
				ID:        "call-1",
				Name:      "failing_tool",
				Arguments: `{}`,
			}},
		},
		{Content: "I corrected the invalid tool call."},
	}}
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	registry, err := tools.NewRegistry(failingTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := New(runService, model, registry, 2)

	run, err := engine.Start(context.Background(), "conversation-1", "trigger failure")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != agentservice.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model request count = %d, want 2", len(model.requests))
	}
	lastMessage := model.requests[1][len(model.requests[1])-1]
	if lastMessage.Role != "tool" || lastMessage.ToolCallID != "call-1" {
		t.Fatalf("tool failure message = %#v", lastMessage)
	}
	var failureResult map[string]any
	if decodeErr := json.Unmarshal([]byte(lastMessage.Content), &failureResult); decodeErr != nil {
		t.Fatalf("decode tool failure message: %v", decodeErr)
	}
	if failureResult["status"] != "error" || failureResult["message"] != "expected tool failure" {
		t.Fatalf("tool failure result = %#v", failureResult)
	}
	events, listErr := engine.ListEvents(context.Background(), run.ID, 0)
	if errors.Is(listErr, agentservice.ErrRunNotFound) {
		t.Fatal("failed run was not persisted")
	}
	if listErr != nil {
		t.Fatalf("ListEvents() error = %v", listErr)
	}
	wantTypes := []agentservice.EventType{
		agentservice.EventRunStarted,
		agentservice.EventToolStarted,
		agentservice.EventToolArgsDelta,
		agentservice.EventToolFailed,
		agentservice.EventMessageStarted,
		agentservice.EventMessageDelta,
		agentservice.EventMessageFinished,
		agentservice.EventRunFinished,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %#v", events)
	}
	for index, event := range events {
		if event.Type != wantTypes[index] {
			t.Fatalf("events[%d].Type = %q, want %q", index, event.Type, wantTypes[index])
		}
	}
}

func TestEngineBindsApprovalToLatestGeneration(t *testing.T) {
	model := &scriptedModel{responses: []agent.ModelResponse{{Content: "已批准。"}}}
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	registry, err := tools.NewRegistry(tools.AskUserTool{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := New(runService, model, registry, 2)
	run, err := runService.StartRun(context.Background(), "conversation-1", "generate")
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	generationID := int64(9)
	run.LatestGenerationID = &generationID
	if saveErr := runService.SaveRun(context.Background(), run); saveErr != nil {
		t.Fatalf("SaveRun() error = %v", saveErr)
	}
	run, pending, err := runService.RequestUserInput(
		context.Background(),
		run.ID,
		agentservice.AskUserRequest{Questions: []agentservice.Question{{
			ID:       "approve_dsl",
			Prompt:   "批准 DSL？",
			Type:     agentservice.QuestionConfirm,
			Required: true,
		}}},
	)
	if err != nil {
		t.Fatalf("RequestUserInput() error = %v", err)
	}

	run, err = engine.Resume(
		context.Background(),
		run.ID,
		pending.ToolCallID,
		agentservice.ResumeToolCallRequest{Answers: map[string]any{"approve_dsl": true}},
	)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if run.ApprovedGenerationID == nil || *run.ApprovedGenerationID != generationID {
		t.Fatalf("approved generation = %#v", run.ApprovedGenerationID)
	}
}
