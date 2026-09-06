package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

type scriptedModel struct {
	responses []agent.ModelResponse
	requests  [][]agent.Message
}

type telemetryModel struct{}

func (telemetryModel) Complete(
	ctx context.Context,
	_ []agent.Message,
	_ []agent.ToolDefinition,
) (agent.ModelResponse, error) {
	telemetry := agent.ModelTelemetry{
		Provider: "test", RequestedModel: "requested", ResolvedModel: "resolved",
		Prompt:   agent.PromptSpec{Version: agent.SystemPromptVersion},
		Usage:    agent.ModelUsage{Status: agent.UsageUnavailable},
		Attempts: []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
	}
	if err := agent.EmitTelemetry(ctx, telemetry, nil); err != nil {
		return agent.ModelResponse{}, err
	}
	return agent.ModelResponse{Content: "done", Telemetry: telemetry}, nil
}

type failingTool struct{}

type cancellableTool struct {
	started chan struct{}
}

type staticResultTool struct {
	name     string
	content  json.RawMessage
	artifact *tools.Artifact
	calls    *int
}

func (t staticResultTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        t.name,
		Description: t.name,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func (t staticResultTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	if t.calls != nil {
		(*t.calls)++
	}
	return tools.Result{Content: t.content, Artifact: t.artifact}, nil
}

func (failingTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "failing_tool",
		Description: "Always fails.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func (t cancellableTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        "cancellable_tool",
		Description: "Blocks until its context is cancelled.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func (t cancellableTool) Execute(ctx context.Context, _ tools.Call) (tools.Result, error) {
	close(t.started)
	<-ctx.Done()
	return tools.Result{}, ctx.Err()
}

func TestSystemPromptRequiresRealSearchControls(t *testing.T) {
	if !strings.Contains(defaultSystemPrompt, "real input step followed by the real search-control click") {
		t.Fatal("system prompt does not require canonical search control actions")
	}
	if !strings.Contains(defaultSystemPrompt, "Never replace those actions with goto") {
		t.Fatal("system prompt does not prohibit direct search URL bypass")
	}
	if !strings.Contains(defaultSystemPrompt, "Omit trigger for ordinary semantic input") ||
		!strings.Contains(defaultSystemPrompt, "search-button action as a separate click step") {
		t.Fatal("system prompt does not define safe input trigger semantics")
	}
}

func TestHarnessPersistsLLMTelemetryBeforeCompletion(t *testing.T) {
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	registry, err := tools.NewRegistry(tools.AskUserTool{})
	if err != nil {
		t.Fatal(err)
	}
	engine := New(runService, telemetryModel{}, registry, 2)
	run, err := engine.Start(context.Background(), "conversation-1", "test")
	if err != nil {
		t.Fatal(err)
	}
	events, err := engine.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[1].Type != agentservice.EventResearchLLMCall ||
		events[1].StepID == "" || events[5].Type != agentservice.EventRunFinished {
		t.Fatalf("events = %#v", events)
	}
	if events[1].ToolCallID != "" ||
		events[1].Payload["tool_call_status"] != string(agentservice.ToolCallUnavailable) ||
		events[1].Payload["tool_call_unavailable_reason"] !=
			string(agentservice.ToolCallUnavailableModelReturnedFinalText) {
		t.Fatalf("final text association = %#v", events[1])
	}
}

func TestBUG131ReplayReachesGenerationWithinExistingBudget(t *testing.T) {
	responses := make([]agent.ModelResponse, 0, 14)
	for index, toolName := range []string{
		"explore_page",
		"explore_page",
		"explore_flow",
		"explore_page",
		"explore_flow",
		"explore_flow",
		"explore_flow",
		"explore_flow",
		"explore_flow",
		"explore_flow",
		"explore_flow",
		"validate_page_elements",
	} {
		responses = append(responses, agent.ModelResponse{
			ToolCalls: []agent.ModelTool{{
				ID:        fmt.Sprintf("replay-%02d", index+1),
				Name:      toolName,
				Arguments: `{}`,
			}},
		})
	}
	responses = append(
		responses,
		agent.ModelResponse{ToolCalls: []agent.ModelTool{{
			ID: "generate-bound", Name: "generate_dsl", Arguments: `{}`,
		}}},
		agent.ModelResponse{ToolCalls: []agent.ModelTool{{
			ID:   "approve-generated",
			Name: "ask_user_question",
			Arguments: `{"questions":[{
				"id":"approve_dsl",
				"question":"批准 DSL？",
				"type":"confirm",
				"required":true
			}]}`,
		}}},
	)
	model := &scriptedModel{responses: responses}
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	registry, err := tools.NewRegistry(
		staticResultTool{
			name:    "explore_page",
			content: json.RawMessage(`{"a11y_nodes":[]}`),
		},
		staticResultTool{
			name:    "explore_flow",
			content: json.RawMessage(`{"pages":[],"success":true}`),
		},
		staticResultTool{
			name:    "validate_page_elements",
			content: json.RawMessage(`{"valid":true}`),
		},
		staticResultTool{
			name:    "generate_dsl",
			content: json.RawMessage(`{"generation_id":131}`),
			artifact: &tools.Artifact{
				Type: "dsl_generation",
				ID:   "131",
			},
		},
		tools.AskUserTool{},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	run, err := New(runService, model, registry, 20).Start(
		context.Background(),
		"14",
		"replay run_847c3b804514fa7459cbd709",
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.Status != agentservice.RunStatusWaitingUser ||
		run.LatestGenerationID == nil ||
		*run.LatestGenerationID != 131 {
		t.Fatalf("run = %#v, want approval checkpoint for generation 131", run)
	}
	if len(model.requests) != 14 {
		t.Fatalf("turns = %d, want 14 within unchanged budget 20", len(model.requests))
	}
}

func TestBUG132ReplayUsesSingleGenerationForSemanticTargets(t *testing.T) {
	responses := []agent.ModelResponse{
		{ToolCalls: []agent.ModelTool{{ID: "explore-1", Name: "explore_page", Arguments: `{}`}}},
		{ToolCalls: []agent.ModelTool{{ID: "explore-2", Name: "explore_flow", Arguments: `{}`}}},
		{ToolCalls: []agent.ModelTool{{ID: "explore-3", Name: "explore_page", Arguments: `{}`}}},
		{ToolCalls: []agent.ModelTool{{ID: "explore-4", Name: "explore_flow", Arguments: `{}`}}},
		{ToolCalls: []agent.ModelTool{{ID: "explore-5", Name: "explore_flow", Arguments: `{}`}}},
		{ToolCalls: []agent.ModelTool{{
			ID:   "generate-semantic",
			Name: "generate_dsl",
			Arguments: `{
				"case":{"name":"BUG-132 replay","steps":[
					{"action":"click","target":"View Cart"}
				]},
				"a11y_nodes_by_state":{"cart":[]}
			}`,
		}}},
		{ToolCalls: []agent.ModelTool{{
			ID:   "approve-generated",
			Name: "ask_user_question",
			Arguments: `{"questions":[{
				"id":"approve_dsl",
				"question":"批准 DSL？",
				"type":"confirm",
				"required":true
			}]}`,
		}}},
	}
	model := &scriptedModel{responses: responses}
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	generationCalls := 0
	registry, err := tools.NewRegistry(
		staticResultTool{name: "explore_page", content: json.RawMessage(`{"a11y_nodes":[]}`)},
		staticResultTool{name: "explore_flow", content: json.RawMessage(`{"pages":[],"success":true}`)},
		staticResultTool{
			name:     "generate_dsl",
			content:  json.RawMessage(`{"generation_id":132}`),
			artifact: &tools.Artifact{Type: "dsl_generation", ID: "132"},
			calls:    &generationCalls,
		},
		tools.AskUserTool{},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	run, err := New(runService, model, registry, 20).Start(
		context.Background(),
		"15",
		"replay run_c77c8c19791d44bc2e761bcc",
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if generationCalls != 1 {
		t.Fatalf("generate_dsl calls = %d, want 1", generationCalls)
	}
	if run.Status != agentservice.RunStatusWaitingUser ||
		run.LatestGenerationID == nil ||
		*run.LatestGenerationID != 132 {
		t.Fatalf("run = %#v, want approval checkpoint for generation 132", run)
	}
	if len(model.requests) != 7 {
		t.Fatalf("turns = %d, want 7", len(model.requests))
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

func TestCancelActiveRunStopsToolWithoutFailureEvents(t *testing.T) {
	model := &scriptedModel{responses: []agent.ModelResponse{{
		ToolCalls: []agent.ModelTool{{
			ID: "call-1", Name: "cancellable_tool", Arguments: `{}`,
		}},
	}}}
	repository := agentservice.NewMemoryRepository()
	runService := agentservice.NewService(repository)
	started := make(chan struct{})
	registry, err := tools.NewRegistry(cancellableTool{started: started})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := New(runService, model, registry, 2)

	run, err := engine.StartOwnedProjectAsync(
		context.Background(),
		7,
		"conversation-1",
		11,
		"run until cancelled",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectAsync() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}

	cancelled, err := engine.CancelOwned(
		context.Background(),
		7,
		run.ID,
		"test timeout",
	)
	if err != nil {
		t.Fatalf("CancelOwned() error = %v", err)
	}
	if cancelled.Status != agentservice.RunStatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}

	deadline := time.Now().Add(time.Second)
	for {
		engine.activeMu.Lock()
		_, active := engine.activeRuns[run.ID]
		engine.activeMu.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled run remained registered")
		}
		time.Sleep(time.Millisecond)
	}
	events, err := engine.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if events[len(events)-1].Type != agentservice.EventRunCancelled {
		t.Fatalf("last event = %#v", events[len(events)-1])
	}
	for _, event := range events {
		if event.Type == agentservice.EventToolFailed ||
			event.Type == agentservice.EventRunFailed {
			t.Fatalf("cancellation emitted failure event: %#v", event)
		}
	}
}

func TestCancelResumedRunUsesRegisteredCancelFunc(t *testing.T) {
	started := make(chan struct{})
	model := &scriptedModel{responses: []agent.ModelResponse{
		{ToolCalls: []agent.ModelTool{{
			ID: "approval-1", Name: "ask_user_question",
			Arguments: `{"questions":[{"id":"continue","question":"继续？","type":"confirm","required":true}]}`,
		}}},
		{ToolCalls: []agent.ModelTool{{
			ID: "call-2", Name: "cancellable_tool", Arguments: `{}`,
		}}},
	}}
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	registry, err := tools.NewRegistry(
		tools.AskUserTool{},
		cancellableTool{started: started},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	engine := New(runService, model, registry, 3)
	run, err := engine.StartOwnedProjectAsync(
		context.Background(),
		7,
		"conversation-1",
		11,
		"pause first",
	)
	if err != nil {
		t.Fatalf("StartOwnedProjectAsync() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for run.Status != agentservice.RunStatusWaitingUser {
		if time.Now().After(deadline) {
			t.Fatal("run did not reach waiting_user")
		}
		time.Sleep(time.Millisecond)
		run, err = engine.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
	}

	resumed := make(chan error, 1)
	go func() {
		_, resumeErr := engine.ResumeOwned(
			context.Background(),
			7,
			run.ID,
			*run.PendingToolCallID,
			agentservice.ResumeToolCallRequest{
				Answers: map[string]any{"continue": true},
			},
		)
		resumed <- resumeErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("resumed tool did not start")
	}
	cancelled, err := engine.CancelOwned(
		context.Background(),
		7,
		run.ID,
		"resume timeout",
	)
	if err != nil {
		t.Fatalf("CancelOwned() error = %v", err)
	}
	if cancelled.Status != agentservice.RunStatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	select {
	case resumeErr := <-resumed:
		if resumeErr != nil {
			t.Fatalf("Resume() error = %v", resumeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Resume() did not stop after cancellation")
	}
}
