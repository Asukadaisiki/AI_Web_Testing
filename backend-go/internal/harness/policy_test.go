package harness

import (
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
)

func TestDefaultToolPolicyRequiresValidationBeforeGeneration(t *testing.T) {
	policy := DefaultToolPolicy{}
	call := agent.ModelTool{Name: "generate_dsl"}
	if err := policy.BeforeToolCall(agentservice.AgentRun{}, call); err == nil {
		t.Fatal("BeforeToolCall() error = nil, want validation gate error")
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

	run.Transcript = append(run.Transcript,
		agent.Message{
			Role: "assistant",
			ToolCalls: []agent.ModelTool{{
				ID:   "validate-2",
				Name: "validate_page_elements",
			}},
		},
		agent.Message{Role: "tool", ToolCallID: "validate-2", Content: `{"valid":false}`},
	)
	if err := policy.BeforeToolCall(run, call); err == nil {
		t.Fatal("BeforeToolCall() error = nil, want latest validation failure")
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
