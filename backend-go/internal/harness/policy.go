package harness

import (
	"encoding/json"
	"errors"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
)

type ToolPolicy interface {
	BeforeToolCall(run agentservice.AgentRun, call agent.ModelTool) error
}

type DefaultToolPolicy struct{}

func (DefaultToolPolicy) BeforeToolCall(run agentservice.AgentRun, call agent.ModelTool) error {
	switch call.Name {
	case "generate_dsl":
		if !hasSuccessfulToolResult(run.Transcript, "validate_page_elements", "valid") {
			return errors.New("generate_dsl requires a successful validate_page_elements result")
		}
	case "execute_dsl":
		if run.ApprovedGenerationID == nil {
			return errors.New("execute_dsl requires an approved DSL generation")
		}
	}
	return nil
}

func hasSuccessfulToolResult(messages []agent.Message, toolName string, booleanField string) bool {
	callNames := make(map[string]string)
	for _, message := range messages {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				callNames[call.ID] = call.Name
			}
			continue
		}
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "tool" || callNames[message.ToolCallID] != toolName {
			continue
		}
		var result map[string]any
		if json.Unmarshal([]byte(message.Content), &result) == nil {
			if value, ok := result[booleanField].(bool); ok {
				return value
			}
		}
	}
	return false
}
