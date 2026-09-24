package agentruntime

import (
	"encoding/json"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

const (
	committedStepsPrefix = "Committed planner steps:\n"
	currentStatePrefix   = "Current planner state:\n"
)

// BudgetView tells the model how much room remains before runtime fuses fire.
type BudgetView struct {
	ModelCalls          int `json:"model_calls"`
	TotalTokens         int `json:"total_tokens"`
	FreshTotalTokens    int `json:"fresh_total_tokens"`
	PromptTokensPerCall int `json:"prompt_tokens_per_call"`
	RequestBytesPerCall int `json:"request_bytes_per_call"`
}

// PlanningContext is the complete input used to construct one fresh model call.
type PlanningContext struct {
	Goal      string
	State     planner.StateSnapshot
	Usage     usage.Usage
	Remaining BudgetView
}

// RequestSizer reports the complete serialized request size for an LLM
// implementation that can construct its transport envelope without sending it.
type RequestSizer interface {
	RequestSize(messages []Message) (int, error)
}

// BuildPlanningMessages constructs a new four-message request from canonical
// state. Protocol messages from previous turns are deliberately not replayed.
func BuildPlanningMessages(context PlanningContext) []Message {
	committed := struct {
		Version int                `json:"version"`
		Steps   []planner.StepView `json:"committed_steps"`
	}{
		Version: context.State.Version,
		Steps:   context.State.Steps,
	}
	current := struct {
		Version    int                        `json:"version"`
		Page       *planner.PageView          `json:"current_page,omitempty"`
		LastResult *planner.CompactResult     `json:"last_result,omitempty"`
		Failures   []planner.FailureSignature `json:"failure_ledger,omitempty"`
		Usage      usage.Usage                `json:"usage"`
		Remaining  BudgetView                 `json:"remaining_budget"`
	}{
		Version:    context.State.Version,
		Page:       context.State.Page,
		LastResult: context.State.LastResult,
		Failures:   context.State.Failures,
		Usage:      context.Usage,
		Remaining:  context.Remaining,
	}

	return []Message{
		{Role: RoleSystem, Content: SystemPrompt()},
		{Role: RoleUser, Content: GoalMessage(context.Goal)},
		{Role: RoleUser, Content: committedStepsPrefix + mustMarshalContext(committed)},
		{Role: RoleUser, Content: currentStatePrefix + mustMarshalContext(current)},
	}
}

func mustMarshalContext(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
