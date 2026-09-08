package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
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

func TestDefaultToolPolicyBlocksExploreFlowWhenFactsAreSufficient(t *testing.T) {
	policy := DefaultToolPolicy{}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com", "S0"), 1),
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 2),
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com/detail", "S2"), 3),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"url":"https://example.com/detail","actions":[{"action":"wait_for","target":"known text"}]}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "facts_sufficient_for_generation") ||
		!strings.Contains(err.Error(), "call generate_dsl") {
		t.Fatalf("BeforeToolCall() error = %v, want facts sufficiency gate", err)
	}
}

func TestDefaultToolPolicyAllowsReexploreAfterGenerationPreflightFailure(t *testing.T) {
	policy := DefaultToolPolicy{}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com", "S0"), 1),
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 2),
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com/detail", "S2"), 3),
		{
			Role:       "tool",
			ToolCallID: "generate-1",
			Content: `{
				"status":"error",
				"tool":"generate_dsl",
				"message":"DSL locator preflight failed: target matched 0 elements in states [detail]"
			}`,
		},
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"url":"https://example.com/detail","actions":[{"action":"wait_for","target":"missing"}]}]}`,
	})
	if err != nil {
		t.Fatalf("BeforeToolCall() error = %v, want one re-explore after preflight failure", err)
	}
}

func TestDefaultToolPolicyKeepsEvidenceGapOpenUntilMissingTargetsAreCovered(t *testing.T) {
	policy := DefaultToolPolicy{}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com", "S0"), 1),
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 2),
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com/detail", "S2"), 3),
		{
			Role:       "tool",
			ToolCallID: "generate-1",
			Content: `{
				"status":"error",
				"tool":"generate_dsl",
				"message":"DSL locator preflight failed: Step 4: target 'Needle Text' matched 0 elements in states [S2]"
			}`,
		},
		explorationSummaryMessage(
			t,
			"explore_page",
			pageResultWithNodeNameJSON("https://example.com/detail", "S2", "Still not the missing fact"),
			4,
		),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"url":"https://example.com/detail","actions":[{"action":"wait_for","target":"Needle Text"}]}]}`,
	})
	if err != nil {
		t.Fatalf("BeforeToolCall() error = %v, want evidence repair exploration to remain allowed", err)
	}
}

func TestDefaultToolPolicyClosesEvidenceGapWhenMissingTargetsAreCovered(t *testing.T) {
	policy := DefaultToolPolicy{}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com", "S0"), 1),
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 2),
		explorationSummaryMessage(t, "explore_page", pageResultJSON("https://example.com/detail", "S2"), 3),
		{
			Role:       "tool",
			ToolCallID: "generate-1",
			Content: `{
				"status":"error",
				"tool":"generate_dsl",
				"message":"DSL locator preflight failed: Step 4: target 'Needle Text' matched 0 elements in states [S2]"
			}`,
		},
		explorationSummaryMessage(
			t,
			"explore_page",
			pageResultWithNodeNameJSON("https://example.com/detail", "S2", "Needle Text"),
			4,
		),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"url":"https://example.com/detail","actions":[{"action":"wait_for","target":"Needle Text"}]}]}`,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "facts_sufficient_for_generation") ||
		!strings.Contains(err.Error(), "unresolved_generation_targets=[]") {
		t.Fatalf("BeforeToolCall() error = %v, want closed evidence gap to trigger generation", err)
	}
}

func TestDefaultToolPolicyBlocksRepeatedExploreFlowSignature(t *testing.T) {
	policy := DefaultToolPolicy{}
	arguments := `{"steps":[{"actions":[{"action":"wait_for","target":" Same Text ","timeout_ms":1000}]}]}`
	repeated := `{"steps":[{"actions":[{"action":"wait_for","target":"same   text","timeout_ms":5000}]}]}`
	run := agentservice.AgentRun{Transcript: []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ModelTool{{Name: "explore_flow", Arguments: arguments}}},
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 1),
		{Role: "assistant", ToolCalls: []agent.ModelTool{{Name: "explore_flow", Arguments: repeated}}},
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: repeated,
	})
	if err == nil || !strings.Contains(err.Error(), "repeated_explore_flow_signature") {
		t.Fatalf("BeforeToolCall() error = %v, want repeated signature gate", err)
	}
}

func TestDefaultToolPolicyLimitsExploreFlowCalls(t *testing.T) {
	policy := DefaultToolPolicy{Exploration: ExplorationGateConfig{
		MaxExploreFlowCalls: 1,
	}}
	run := agentservice.AgentRun{Transcript: []agent.Message{
		explorationSummaryMessage(t, "explore_flow", flowResultJSON("https://example.com/products", "S1"), 1),
	}}

	err := policy.BeforeToolCall(run, agent.ModelTool{
		Name:      "explore_flow",
		Arguments: `{"steps":[{"actions":[{"action":"wait_for","target":"next"}]}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "explore_flow_budget_exhausted") {
		t.Fatalf("BeforeToolCall() error = %v, want budget gate", err)
	}
}

func explorationSummaryMessage(
	t *testing.T,
	tool string,
	raw string,
	seq int64,
) agent.Message {
	t.Helper()
	content, err := agent.BuildModelToolSummary(tool, json.RawMessage(raw), seq)
	if err != nil {
		t.Fatal(err)
	}
	return agent.Message{Role: "tool", ToolCallID: tool, Content: content}
}

func pageResultJSON(url string, state string) string {
	return pageResultWithNodeNameJSON(url, state, "Continue")
}

func pageResultWithNodeNameJSON(url string, state string, nodeName string) string {
	return `{
		"url":"` + url + `",
		"page_state":"` + state + `",
		"status":"success",
		"element_count":2,
		"a11y_nodes":[{
			"node_id":"n1",
			"role":"button",
			"name":"` + nodeName + `",
			"page_state":"` + state + `",
			"focusable":true,
			"verified_selectors":[{"strategy":"css","selector":"#continue"}]
		}]
	}`
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
