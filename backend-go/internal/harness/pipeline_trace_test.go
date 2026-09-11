package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

func TestNormalizedToolCallSignatureUsesSemanticExploreArguments(t *testing.T) {
	first := agent.ModelTool{
		Name: "explore_flow",
		Arguments: `{
			"flow_description":"first wording",
			"plan_step_ids":["s1"],
			"steps":[{
				"description":"first step",
				"actions":[{"action":"wait_for","target":" Same Text ","timeout_ms":1000}]
			}]
		}`,
	}
	second := agent.ModelTool{
		Name: "explore_flow",
		Arguments: `{
			"flow_description":"other wording",
			"plan_step_ids":["s1"],
			"steps":[{
				"description":"other step",
				"actions":[{"action":"wait_for","target":"same   text","timeout_ms":5000}]
			}]
		}`,
	}
	if normalizedToolCallSignature(first) != normalizedToolCallSignature(second) {
		t.Fatal("semantic-equivalent explore calls produced different signatures")
	}

	first.Name = "generate_dsl"
	second.Name = "generate_dsl"
	if normalizedToolCallSignature(first) == normalizedToolCallSignature(second) {
		t.Fatal("non-explore tool signatures ignored distinct arguments")
	}
}

func TestPipelineStateEpochChangesWithPlanAndGeneration(t *testing.T) {
	run := agentservice.AgentRun{Status: agentservice.RunStatusRunning}
	plan := &taskplan.Plan{
		ID: "plan-1", Version: 1, PlanSHA256: strings.Repeat("a", 64),
		Status: taskplan.StatusGrounding,
	}
	first := pipelineStateEpoch(run, plan)
	plan.Version = 2
	second := pipelineStateEpoch(run, plan)
	generationID := int64(7)
	run.LatestGenerationID = &generationID
	third := pipelineStateEpoch(run, plan)
	if first == second || second == third {
		t.Fatalf("state epochs did not change: %q %q %q", first, second, third)
	}
}

func TestToolPlanStepIDsCollectsNestedDSLBindings(t *testing.T) {
	ids := toolPlanStepIDs(`{
		"plan_step_ids":["open"],
		"case":{"steps":[
			{"plan_step_id":"open"},
			{"plan_step_id":"submit"}
		]}
	}`)
	if strings.Join(ids, ",") != "open,submit" {
		t.Fatalf("plan step ids = %#v", ids)
	}
}

func TestToolTraceAttemptLinksSameSignatureAndStateEpoch(t *testing.T) {
	ctx := context.Background()
	runService := agentservice.NewService(agentservice.NewMemoryRepository())
	run, err := runService.StartRun(ctx, "conversation-1", "goal")
	if err != nil {
		t.Fatal(err)
	}
	engine := &Harness{runs: runService}
	plan := &taskplan.Plan{
		ID: "plan-1", Version: 1, PlanSHA256: strings.Repeat("a", 64),
		Status: taskplan.StatusGrounding,
	}
	firstCall := agent.ModelTool{
		ID: "call-1", Name: "explore_page",
		Arguments: `{"url":"https://example.test","plan_step_ids":["open"]}`,
	}
	first, err := engine.newToolTraceIdentity(ctx, run, firstCall, plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.attempt != 1 || first.retryOfToolCallID != "" {
		t.Fatalf("first identity = %#v", first)
	}
	if err := engine.recordPipelineToolTrace(
		ctx,
		run,
		"step-1",
		firstCall,
		first,
		"failed",
		"tool_execution_failed",
	); err != nil {
		t.Fatal(err)
	}

	secondCall := firstCall
	secondCall.ID = "call-2"
	second, err := engine.newToolTraceIdentity(ctx, run, secondCall, plan)
	if err != nil {
		t.Fatal(err)
	}
	if second.attempt != 2 || second.retryOfToolCallID != "call-1" {
		t.Fatalf("second identity = %#v", second)
	}
}
