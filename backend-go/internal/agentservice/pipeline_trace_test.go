package agentservice

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestRecordPipelineTracePersistsSchemaValidatedPayload(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "goal")
	if err != nil {
		t.Fatal(err)
	}
	payload := PipelineTracePayload{
		Kind:       PipelineTraceToolCall,
		StateEpoch: strings.Repeat("a", 64),
		Plan: &PipelinePlanRef{
			PlanID: "plan-1", Version: 2, SHA256: strings.Repeat("b", 64),
			Status: "grounding",
		},
		ToolCall: &PipelineToolCallTrace{
			Name: "explore_flow", Signature: strings.Repeat("c", 64),
			Status: "failed", Attempt: 2, RetryOfToolCallID: "call-1",
			ReasonCode: "tool_execution_failed", PlanStepIDs: []string{"s1", "s2"},
		},
	}
	if err := service.RecordPipelineTrace(
		context.Background(),
		run,
		"step-2",
		"call-2",
		payload,
	); err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(context.Background(), run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventPipelineTrace {
		t.Fatalf("events = %#v", events)
	}
	validatePipelinePayloadWithSharedSchema(t, events[0].Payload)
}

func TestRecordPipelineTraceRejectsInvalidPayload(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "goal")
	if err != nil {
		t.Fatal(err)
	}
	err = service.RecordPipelineTrace(
		context.Background(),
		run,
		"step-1",
		"call-1",
		PipelineTracePayload{
			Kind:       PipelineTraceToolCall,
			StateEpoch: "invalid",
			ToolCall: &PipelineToolCallTrace{
				Name: "explore_page", Signature: strings.Repeat("c", 64),
				Status: "proposed", Attempt: 1, PlanStepIDs: []string{},
			},
		},
	)
	if err == nil {
		t.Fatal("invalid state epoch was accepted")
	}
}

func TestModelPipelineTraceCarriesRequestPartitions(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "goal")
	if err != nil {
		t.Fatal(err)
	}
	tokens := int64(11)
	err = service.RecordModelTelemetry(context.Background(), run, agent.TelemetryRecord{
		LogicalCallID: "llm-1",
		StepID:        "step-1",
		State: agent.PipelineState{
			Epoch: strings.Repeat("a", 64), PlanID: "plan-1", Version: 1,
			PlanSHA256: strings.Repeat("b", 64), Status: "grounding",
		},
		Telemetry: agent.ModelTelemetry{
			Provider: "provider", RequestedModel: "model",
			Prompt: agent.PromptSpec{
				RequestBudget: agent.RequestSerializationBudget{
					RequestBytes: 100, MessageBytes: 80, ToolDefinitionBytes: 20,
					MessageCount: 4, SystemContentBytes: 10, UserContentBytes: 5,
					AssistantContentBytes: 7, AssistantReasoningBytes: 9,
					AssistantToolArgumentBytes: 11, ToolContentBytes: 13,
					ExplorationSummaryBytes: 3, ExplorationSummaryCount: 1,
					NonExplorationSummaryBytes: 4, RecoverableToolErrorBytes: 2,
				},
			},
			Usage: agent.ModelUsage{
				Status: agent.UsageAvailable, InputTokens: &tokens,
				OutputTokens: &tokens, TotalTokens: &tokens,
			},
			Attempts: []agent.ModelAttempt{{Attempt: 1, Status: "succeeded"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	traces := eventsByType(events, EventPipelineTrace)
	if len(traces) != 1 {
		t.Fatalf("pipeline traces = %#v", traces)
	}
	request := traces[0].Payload["model_request"].(map[string]any)
	budget := request["request_budget"].(map[string]any)
	if budget["message_count"] != float64(4) ||
		budget["assistant_reasoning_bytes"] != float64(9) ||
		budget["non_exploration_summary_bytes"] != float64(4) {
		t.Fatalf("request budget = %#v", budget)
	}
	validatePipelinePayloadWithSharedSchema(t, traces[0].Payload)
}

func TestSummarizePipelineTraceReportsGrowthAndRepeatedCalls(t *testing.T) {
	service := NewService(NewMemoryRepository())
	run, err := service.StartRun(context.Background(), "conversation-1", "goal")
	if err != nil {
		t.Fatal(err)
	}
	plan := &PipelinePlanRef{
		PlanID: "plan-1", Version: 1, SHA256: strings.Repeat("b", 64),
		Status: "grounding",
	}
	for index, requestBytes := range []int{100, 240} {
		err := service.RecordPipelineTrace(
			context.Background(),
			run,
			"model-step",
			"",
			PipelineTracePayload{
				Kind: PipelineTraceModelRequest, StateEpoch: strings.Repeat("a", 64),
				Plan: plan,
				ModelRequest: &PipelineModelRequestTrace{
					LogicalCallID: "llm-" + string(rune('1'+index)),
					Attempt:       1,
					AttemptStatus: "succeeded",
					RequestBudget: agent.RequestSerializationBudget{
						RequestBytes: requestBytes, MessageBytes: requestBytes - 20,
						MessageCount: index + 1,
					},
					Cumulative: PipelineCumulativeUsage{
						LogicalCalls: index + 1, PhysicalAttempts: index + 1,
						TotalTokens: int64((index + 1) * 10),
					},
					ToolCallIDs: []string{},
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	for attempt, callID := range []string{"call-1", "call-2"} {
		err := service.RecordPipelineTrace(
			context.Background(),
			run,
			"tool-step",
			callID,
			PipelineTracePayload{
				Kind: PipelineTraceToolCall, StateEpoch: strings.Repeat("a", 64),
				Plan: plan,
				ToolCall: &PipelineToolCallTrace{
					Name: "explore_page", Signature: strings.Repeat("c", 64),
					Status: "proposed", Attempt: attempt + 1,
					RetryOfToolCallID: map[bool]string{true: "call-1"}[attempt > 0],
					PlanStepIDs:       []string{"s1"},
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := service.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizePipelineTrace(run.ID, events)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TraceEvents != 4 ||
		summary.ModelRequests != 2 ||
		summary.ToolCalls != 2 ||
		len(summary.RepeatedToolCalls) != 1 ||
		summary.ContextGrowth.FirstRequestBytes != 100 ||
		summary.ContextGrowth.LatestRequestBytes != 240 ||
		summary.ContextGrowth.MaxMessageBytes != 220 ||
		summary.Cumulative.LogicalCalls != 2 ||
		summary.Cumulative.TotalTokens != 20 ||
		len(summary.PlanVersions) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func validatePipelinePayloadWithSharedSchema(t *testing.T, payload map[string]any) {
	t.Helper()
	content, err := os.ReadFile(
		filepath.Join("..", "..", "..", "contracts", "agent-pipeline-trace.v1.schema.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource(
		"https://ai-web-testing.local/contracts/agent-pipeline-trace.v1.schema.json",
		bytes.NewReader(content),
	); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(
		"https://ai-web-testing.local/contracts/agent-pipeline-trace.v1.schema.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
}
