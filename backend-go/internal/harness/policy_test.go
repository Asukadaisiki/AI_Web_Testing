package harness

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

func TestDefaultToolPolicyDoesNotTrustAdvisoryValidationForGeneration(t *testing.T) {
	policy := DefaultToolPolicy{}
	call := agent.ModelTool{Name: "generate_dsl"}
	if err := policy.BeforeToolCall(agentservice.AgentRun{}, call); err != nil {
		t.Fatalf("BeforeToolCall() error = %v", err)
	}

	run := agentservice.AgentRun{Transcript: []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ModelTool{{
				ID:   "validate-1",
				Name: "validate_page_elements",
			}},
		},
		{Role: "tool", ToolCallID: "validate-1", Content: `{"valid":true}`},
	}}
	if err := policy.BeforeToolCall(run, call); err != nil {
		t.Fatalf("BeforeToolCall() error = %v", err)
	}
}

func TestDefaultToolPolicyRequiresApprovalBeforeExecution(t *testing.T) {
	policy := DefaultToolPolicy{}
	call := agent.ModelTool{Name: "execute_dsl"}
	if err := policy.BeforeToolCall(agentservice.AgentRun{}, call); err == nil {
		t.Fatal("BeforeToolCall() error = nil, want approval gate error")
	}

	generationID := int64(42)
	run := agentservice.AgentRun{ApprovedGenerationID: &generationID}
	if err := policy.BeforeToolCall(run, call); err != nil {
		t.Fatalf("BeforeToolCall() error = %v", err)
	}
}

func TestDefaultToolPolicyBlocksRepeatedExploreFlowSignature(t *testing.T) {
	policy := DefaultToolPolicy{}
	arguments := `{"flow_description":"first wording","steps":[{"description":"first step wording","actions":[{"action":"wait_for","locator":{"kind":"text","value":" Same Text ","exact":true},"timeout_ms":1000}]}]}`
	repeated := `{"flow_description":"different wording","steps":[{"description":"different step wording","actions":[{"action":"wait_for","locator":{"kind":"text","value":"same   text","exact":true},"timeout_ms":5000}]}]}`
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{
			ID: "flow-1", Name: "explore_flow", Arguments: arguments,
		}}},
		explorationSummaryMessage(
			t,
			"flow-1",
			"explore_flow",
			flowResultJSON("https://example.com/products", "S1"),
			1,
		),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: repeated,
	})
	if err == nil || !strings.Contains(err.Error(), "repeated_explore_flow_signature") {
		t.Fatalf("BeforeToolCall() error = %v, want repeated signature gate", err)
	}
}

func TestDefaultToolPolicyBlocksRepeatedExplorePageSignature(t *testing.T) {
	policy := DefaultToolPolicy{}
	arguments := `{"url":"https://example.com/form","plan_step_ids":["open"]}`
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{
			ID: "page-1", Name: "explore_page", Arguments: arguments,
		}}},
		explorationSummaryMessage(
			t,
			"page-1",
			"explore_page",
			flowResultJSON("https://example.com/form", "S0"),
			1,
		),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name: "explore_page",
		Arguments: `{
			"url":"https://example.com/form",
			"core_user_flow_text":"different wording",
			"plan_step_ids":["open"]
		}`,
	})
	if err == nil || !strings.Contains(err.Error(), "repeated_explore_page_signature") {
		t.Fatalf("BeforeToolCall() error = %v, want repeated page signature gate", err)
	}
}

func TestDefaultToolPolicyLimitsExploreFlowCalls(t *testing.T) {
	policy := DefaultToolPolicy{Exploration: ExplorationGateConfig{
		MaxExploreFlowCalls: 1,
	}}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(
			t,
			"flow-1",
			"explore_flow",
			flowResultJSON("https://example.com/products", "S1"),
			1,
		),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"actions":[{"action":"wait_for","locator":{"kind":"text","value":"next","exact":true}}]}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "explore_flow_budget_exhausted") {
		t.Fatalf("BeforeToolCall() error = %v, want budget gate", err)
	}
	var gateError *ExplorationGateError
	if !errors.As(err, &gateError) ||
		gateError.Budget.ExploreFlow.Remaining != 0 {
		t.Fatalf("gate error = %#v", gateError)
	}
}

func TestDefaultToolPolicyUsesPerPlanBudgetWithRunHardLimit(t *testing.T) {
	policy := DefaultToolPolicy{Exploration: ExplorationGateConfig{
		MaxExploreFlowCalls:    1,
		MaxRunExploreFlowCalls: 2,
	}}
	oldPlan := &agent.ToolResultTaskPlanSummary{
		PlanID: "plan-old", Version: 1,
	}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{
			ID: "old-flow", Name: "explore_flow",
			Arguments: `{"plan_step_ids":["old"],"steps":[{"actions":[]}]}`,
		}}},
		explorationSummaryMessageWithPlan(
			t,
			"old-flow",
			"explore_flow",
			flowResultJSON("https://example.com/old", "S1"),
			1,
			oldPlan,
		),
	}}
	currentPlan := &taskplan.Plan{ID: "plan-new", Version: 2}
	call := agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"plan_step_ids":["new"],"steps":[{"actions":[]}]}`,
	}
	if err := policy.BeforeToolCall(run, call, currentPlan); err != nil {
		t.Fatalf("new plan should have a fresh plan budget: %v", err)
	}

	policy.Exploration.MaxRunExploreFlowCalls = 1
	err := policy.BeforeToolCall(run, call, currentPlan)
	if err == nil || !strings.Contains(err.Error(), "run_explore_flow_budget_exhausted") {
		t.Fatalf("BeforeToolCall() error = %v, want run hard limit", err)
	}
}

func TestDefaultToolPolicyRejectsExplorationNearWallClockDeadline(t *testing.T) {
	created := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	now := created.Add(5 * time.Minute)
	policy := DefaultToolPolicy{
		Exploration: ExplorationGateConfig{
			// deadline = created + 6m = 09:06; now + reserve = 09:07 > deadline.
			MaxRunWallTime: 6 * time.Minute,
			ExploreReserve: 2 * time.Minute,
		},
		Now: func() time.Time { return now },
	}
	run := agentservice.AgentRun{CreatedAt: created}

	for _, name := range []string{"explore_page", "explore_flow"} {
		err := policy.BeforeToolCall(run, agent.ModelTool{
			Name:      name,
			Arguments: `{}`,
		})
		if err == nil || !strings.Contains(err.Error(), "explore_deadline_budget_exhausted") {
			t.Fatalf("%s: BeforeToolCall() error = %v, want deadline budget exhausted", name, err)
		}
		var gateError *ExplorationGateError
		if !errors.As(err, &gateError) {
			t.Fatalf("%s: error is not an ExplorationGateError: %#v", name, err)
		}
		if gateError.NextAction == "" {
			t.Fatalf("%s: NextAction is empty", name)
		}
		if gateError.Budget == nil {
			t.Fatalf("%s: budget summary is nil", name)
		}
	}
}

func TestDefaultToolPolicyAllowsExplorationWithinWallClockDeadline(t *testing.T) {
	now := time.Date(2025, 1, 1, 9, 4, 0, 0, time.UTC)
	policy := DefaultToolPolicy{
		Exploration: ExplorationGateConfig{
			MaxRunWallTime: 6 * time.Minute,
			ExploreReserve: 2 * time.Minute,
		},
		Now: func() time.Time { return now },
	}
	run := agentservice.AgentRun{
		CreatedAt: now.Add(-2 * time.Minute),
	}
	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"plan_step_ids":["open"],"steps":[{"actions":[]}]}`,
	})
	if err != nil {
		t.Fatalf("BeforeToolCall() error = %v, want exploration allowed within deadline", err)
	}
}

func TestExplorationBudgetExposesRemainingAndCompletedSignature(t *testing.T) {
	policy := DefaultToolPolicy{Exploration: ExplorationGateConfig{
		MaxExplorePageCalls:                  2,
		MaxExploreFlowCalls:                  3,
		MaxRunExplorePageCalls:               4,
		MaxRunExploreFlowCalls:               6,
		MaxRepeatedExploreFlowSignatureCalls: 1,
	}}
	plan := &taskplan.Plan{ID: "plan-1", Version: 3}
	call := agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"flow_description":"first","plan_step_ids":["s1"],"steps":[{"actions":[]}]}`,
	}
	budget := policy.ExplorationBudget(
		agentservice.AgentRun{},
		plan,
		&call,
	)
	if budget.PlanID != "plan-1" ||
		budget.PlanVersion != 3 ||
		budget.ExploreFlow.Used != 1 ||
		budget.ExploreFlow.Remaining != 2 ||
		budget.RunExploreFlow.Used != 1 ||
		budget.CurrentSignatureUses == nil ||
		*budget.CurrentSignatureUses != 1 ||
		budget.CurrentSignatureRemaining == nil ||
		*budget.CurrentSignatureRemaining != 0 {
		t.Fatalf("budget = %#v", budget)
	}
}

func explorationSummaryMessage(
	t *testing.T,
	callID string,
	tool string,
	raw string,
	seq int64,
) agent.Message {
	return explorationSummaryMessageWithPlan(t, callID, tool, raw, seq, nil)
}

func explorationSummaryMessageWithPlan(
	t *testing.T,
	callID string,
	tool string,
	raw string,
	seq int64,
	plan *agent.ToolResultTaskPlanSummary,
) agent.Message {
	t.Helper()
	content, err := agent.BuildModelToolSummary(
		tool,
		json.RawMessage(raw),
		seq,
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}
	return agent.Message{Role: "tool", ToolCallID: callID, Content: content}
}

func flowResultJSON(url string, state string) string {
	return `{
		"success":true,
		"pages":[{
			"url":"` + url + `",
			"page_state":"` + state + `",
			"status":"success",
			"element_count":2,
			"a11y_nodes":[{
				"node_id":"n2",
				"role":"link",
				"name":"Details",
				"page_state":"` + state + `",
				"focusable":true,
				"verified_selectors":[{"strategy":"css","selector":"a.details"}]
			}],
			"actions":[{
				"action":"click",
				"target":"Details",
				"status":"success",
				"page_state":"` + state + `",
				"target_evidence":[{
					"node_id":"n2",
					"role":"link",
					"name":"Details",
					"page_state":"` + state + `",
					"focusable":true,
					"verified_selectors":[{"strategy":"css","selector":"a.details"}]
				}]
			}]
		}]
	}`
}

func TestDefaultToolPolicyBlocksRepeatedFailedGovernanceSignature(t *testing.T) {
	policy := DefaultToolPolicy{}
	arguments := `{
		"goal":"goal",
		"max_side_effect":"browser_state",
		"forbidden_actions":[],
		"steps":[{"id":"open","intent":"Open","action":"goto","value":"https://example.test","expected_occurrences":1,"idempotency":"idempotent","side_effect":"browser_state","preconditions":[],"completion_conditions":[]}]
	}`
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{
			ID: "plan-1", Name: "set_task_plan", Arguments: arguments,
		}}},
		{Role: "tool", ToolCallID: "plan-1",
			Content: `{"status":"error","tool":"set_task_plan","message":"boom"}`},
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "set_task_plan",
		Arguments: arguments,
	})
	if err == nil || !strings.Contains(err.Error(), "repeated_set_task_plan_signature") {
		t.Fatalf("BeforeToolCall() error = %v, want repeated governance signature gate", err)
	}

	// A changed signature is allowed to retry.
	changed := `{
		"goal":"goal",
		"max_side_effect":"browser_state",
		"forbidden_actions":[],
		"steps":[{"id":"open","intent":"Open differently","action":"goto","value":"https://example.test/next","expected_occurrences":1,"idempotency":"idempotent","side_effect":"browser_state","preconditions":[],"completion_conditions":[]}]
	}`
	if err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "set_task_plan",
		Arguments: changed,
	}); err != nil {
		t.Fatalf("changed governance signature should be allowed: %v", err)
	}
}

func TestDefaultToolPolicyAllowsRepeatedSucceededGovernanceSignature(t *testing.T) {
	policy := DefaultToolPolicy{}
	arguments := `{"case":{"name":"replay","steps":[{"action":"click","target":"#pay"}]},"a11y_nodes_by_state":{"S0":[]}}`
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{
			ID: "gen-1", Name: "generate_dsl", Arguments: arguments,
		}}},
		{Role: "tool", ToolCallID: "gen-1",
			Content: `{"success":true,"generation_id":42}`},
	}}
	// The identical signature already succeeded: the policy allows the retry
	// (the control plane deduplicates the result); the ledger only blocks
	// failed/rejected repeats so it cannot dead-end a legitimate resubmit.
	if err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "generate_dsl",
		Arguments: arguments,
	}); err != nil {
		t.Fatalf("succeeded governance signature should not be blocked: %v", err)
	}
}
