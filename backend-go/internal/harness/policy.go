package harness

import (
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
	case "execute_dsl":
		if run.ApprovedGenerationID == nil {
			return errors.New("execute_dsl requires an approved DSL generation")
		}
	}
	return nil
}
