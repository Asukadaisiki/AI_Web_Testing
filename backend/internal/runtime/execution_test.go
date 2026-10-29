package runtime

import (
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func TestBuildHarnessRequestCopiesExecution(t *testing.T) {
	command := commandFor("execution")
	command.Execution = &execution.Spec{RuntimeID: "claude-code", Harness: execution.HarnessSnapshot{
		ID: "generic", Name: "Generic", Revision: 1, Skills: []execution.Skill{{Name: "notes", Content: "original"}},
		MCP: map[string]execution.MCPServer{},
	}}
	request := buildHarnessRequest(command, "", "attempt", "high")
	if request.Execution == nil || request.Execution.RuntimeID != "claude-code" {
		t.Fatal("request omitted selected execution")
	}
	request.Execution.Harness.Skills[0].Content = "mutated"
	if command.Execution.Harness.Skills[0].Content != "original" {
		t.Fatal("runtime request mutated durable command")
	}
}
