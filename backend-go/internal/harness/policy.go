package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type ToolPolicy interface {
	BeforeToolCall(
		run agentservice.AgentRun,
		call agent.ModelTool,
		currentPlan ...*taskplan.Plan,
	) error
}

type explorationBudgetProvider interface {
	ExplorationBudget(
		run agentservice.AgentRun,
		currentPlan *taskplan.Plan,
		completedCall *agent.ModelTool,
	) *agent.ToolResultExplorationBudgetSummary
}

type DefaultToolPolicy struct {
	Exploration ExplorationGateConfig
}

type ExplorationGateConfig struct {
	MaxExplorePageCalls                  int
	MaxExploreFlowCalls                  int
	MaxRunExplorePageCalls               int
	MaxRunExploreFlowCalls               int
	MaxRepeatedExplorePageSignatureCalls int
	MaxRepeatedExploreFlowSignatureCalls int
}

type explorationState struct {
	PlanExplorePageCalls int
	PlanExploreFlowCalls int
	RunExplorePageCalls  int
	RunExploreFlowCalls  int
	PlanSignatures       map[string]int
}

func (c ExplorationGateConfig) withDefaults() ExplorationGateConfig {
	if c.MaxExplorePageCalls <= 0 {
		c.MaxExplorePageCalls = 5
	}
	if c.MaxExploreFlowCalls <= 0 {
		c.MaxExploreFlowCalls = 4
	}
	if c.MaxRunExplorePageCalls <= 0 {
		c.MaxRunExplorePageCalls = 10
	}
	if c.MaxRunExploreFlowCalls <= 0 {
		c.MaxRunExploreFlowCalls = 8
	}
	if c.MaxRepeatedExplorePageSignatureCalls <= 0 {
		c.MaxRepeatedExplorePageSignatureCalls = 1
	}
	if c.MaxRepeatedExploreFlowSignatureCalls <= 0 {
		c.MaxRepeatedExploreFlowSignatureCalls = 1
	}
	return c
}

func (p DefaultToolPolicy) BeforeToolCall(
	run agentservice.AgentRun,
	call agent.ModelTool,
	currentPlan ...*taskplan.Plan,
) error {
	switch call.Name {
	case "execute_dsl":
		if run.ApprovedGenerationID == nil {
			return errors.New("execute_dsl requires an approved DSL generation")
		}
	case "explore_page", "explore_flow":
		return p.beforeExplorationCall(run, call, firstPlan(currentPlan))
	}
	return nil
}

func (p DefaultToolPolicy) beforeExplorationCall(
	run agentservice.AgentRun,
	call agent.ModelTool,
	currentPlan *taskplan.Plan,
) error {
	config := p.Exploration.withDefaults()
	state := buildExplorationState(run.Transcript, currentPlan)
	budget := explorationBudgetSummary(config, state, currentPlan, &call, false)
	switch call.Name {
	case "explore_page":
		if state.RunExplorePageCalls >= config.MaxRunExplorePageCalls {
			return newExplorationGateError(
				"run_explore_page_budget_exhausted",
				budget,
				"the run-wide hard limit is exhausted; do not revise the plan to obtain more probes",
			)
		}
		if state.PlanExplorePageCalls >= config.MaxExplorePageCalls {
			return newExplorationGateError(
				"explore_page_budget_exhausted",
				budget,
				"stop probing; use the persisted TaskPlan state to generate only if all steps are grounded, otherwise ask the user or revise the plan",
			)
		}
	case "explore_flow":
		if state.RunExploreFlowCalls >= config.MaxRunExploreFlowCalls {
			return newExplorationGateError(
				"run_explore_flow_budget_exhausted",
				budget,
				"the run-wide hard limit is exhausted; do not revise the plan to obtain more probes",
			)
		}
		if state.PlanExploreFlowCalls >= config.MaxExploreFlowCalls {
			return newExplorationGateError(
				"explore_flow_budget_exhausted",
				budget,
				"stop probing; generate only when the persisted TaskPlan is ready, otherwise ask the user or revise the plan",
			)
		}
	}
	signature := explorationCallSignature(call.Name, call.Arguments)
	limit := repeatedSignatureLimit(config, call.Name)
	if signature != "" && state.PlanSignatures[signature] >= limit {
		return newExplorationGateError(
			"repeated_"+call.Name+"_signature",
			budget,
			"do not repeat the same probe; change the page, actions, or bound plan steps",
		)
	}
	return nil
}

func buildExplorationState(
	transcript []agent.Message,
	currentPlan *taskplan.Plan,
) explorationState {
	state := explorationState{
		PlanSignatures: make(map[string]int),
	}
	calls := make(map[string]agent.ModelTool)
	for _, message := range transcript {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				if agent.IsExplorationTool(call.Name) && call.ID != "" {
					calls[call.ID] = call
				}
			}
			continue
		}
		if message.Role != "tool" {
			continue
		}
		summary, ok := agent.DecodeModelToolSummary(message.Content)
		if !ok || !agent.IsExplorationTool(summary.Tool) {
			continue
		}
		if summary.Tool == "explore_page" {
			state.RunExplorePageCalls++
		} else {
			state.RunExploreFlowCalls++
		}
		if !summaryMatchesPlan(summary, currentPlan) {
			continue
		}
		if summary.Tool == "explore_page" {
			state.PlanExplorePageCalls++
		} else {
			state.PlanExploreFlowCalls++
		}
		if call, exists := calls[message.ToolCallID]; exists {
			signature := explorationCallSignature(call.Name, call.Arguments)
			if signature != "" {
				state.PlanSignatures[signature]++
			}
		}
	}
	return state
}

type ExplorationGateError struct {
	Code       string
	Budget     *agent.ToolResultExplorationBudgetSummary
	NextAction string
}

func (e *ExplorationGateError) Error() string {
	return fmt.Sprintf(
		"exploration gate %s: plan_page=%d/%d plan_flow=%d/%d run_page=%d/%d run_flow=%d/%d; %s",
		e.Code,
		e.Budget.ExplorePage.Used,
		e.Budget.ExplorePage.Limit,
		e.Budget.ExploreFlow.Used,
		e.Budget.ExploreFlow.Limit,
		e.Budget.RunExplorePage.Used,
		e.Budget.RunExplorePage.Limit,
		e.Budget.RunExploreFlow.Used,
		e.Budget.RunExploreFlow.Limit,
		e.NextAction,
	)
}

func newExplorationGateError(
	code string,
	budget *agent.ToolResultExplorationBudgetSummary,
	nextAction string,
) error {
	return &ExplorationGateError{
		Code:       code,
		Budget:     budget,
		NextAction: nextAction,
	}
}

func (p DefaultToolPolicy) ExplorationBudget(
	run agentservice.AgentRun,
	currentPlan *taskplan.Plan,
	completedCall *agent.ModelTool,
) *agent.ToolResultExplorationBudgetSummary {
	config := p.Exploration.withDefaults()
	state := buildExplorationState(run.Transcript, currentPlan)
	return explorationBudgetSummary(
		config,
		state,
		currentPlan,
		completedCall,
		true,
	)
}

func explorationBudgetSummary(
	config ExplorationGateConfig,
	state explorationState,
	currentPlan *taskplan.Plan,
	currentCall *agent.ModelTool,
	countCompleted bool,
) *agent.ToolResultExplorationBudgetSummary {
	signature := ""
	if currentCall != nil && agent.IsExplorationTool(currentCall.Name) {
		if countCompleted && currentCall.Name == "explore_page" {
			state.PlanExplorePageCalls++
			state.RunExplorePageCalls++
		} else if countCompleted {
			state.PlanExploreFlowCalls++
			state.RunExploreFlowCalls++
		}
		signature = explorationCallSignature(
			currentCall.Name,
			currentCall.Arguments,
		)
		if countCompleted && signature != "" {
			state.PlanSignatures[signature]++
		}
	}
	result := &agent.ToolResultExplorationBudgetSummary{
		Scope:                       "run",
		ExplorePage:                 budgetCounter(state.PlanExplorePageCalls, config.MaxExplorePageCalls),
		ExploreFlow:                 budgetCounter(state.PlanExploreFlowCalls, config.MaxExploreFlowCalls),
		RunExplorePage:              budgetCounter(state.RunExplorePageCalls, config.MaxRunExplorePageCalls),
		RunExploreFlow:              budgetCounter(state.RunExploreFlowCalls, config.MaxRunExploreFlowCalls),
		RepeatedExplorePageLimit:    config.MaxRepeatedExplorePageSignatureCalls,
		RepeatedExploreFlowLimit:    config.MaxRepeatedExploreFlowSignatureCalls,
		CurrentTool:                 toolName(currentCall),
		CurrentSignatureFingerprint: signature,
	}
	if signature != "" {
		uses := state.PlanSignatures[signature]
		signatureRemaining := remaining(
			repeatedSignatureLimit(config, toolName(currentCall)),
			uses,
		)
		result.CurrentSignatureUses = &uses
		result.CurrentSignatureRemaining = &signatureRemaining
	}
	if currentPlan != nil {
		result.Scope = "plan_version"
		result.PlanID = currentPlan.ID
		result.PlanVersion = currentPlan.Version
	}
	return result
}

func budgetCounter(used int, limit int) agent.ToolResultBudgetCounter {
	return agent.ToolResultBudgetCounter{
		Used: used, Limit: limit, Remaining: remaining(limit, used),
	}
}

func remaining(limit int, used int) int {
	if used >= limit {
		return 0
	}
	return limit - used
}

func toolName(call *agent.ModelTool) string {
	if call == nil {
		return ""
	}
	return call.Name
}

func repeatedSignatureLimit(config ExplorationGateConfig, tool string) int {
	if tool == "explore_page" {
		return config.MaxRepeatedExplorePageSignatureCalls
	}
	return config.MaxRepeatedExploreFlowSignatureCalls
}

func firstPlan(plans []*taskplan.Plan) *taskplan.Plan {
	if len(plans) == 0 {
		return nil
	}
	return plans[0]
}

func summaryMatchesPlan(
	summary agent.ModelToolSummary,
	currentPlan *taskplan.Plan,
) bool {
	if currentPlan == nil {
		return true
	}
	return summary.TaskPlan != nil &&
		summary.TaskPlan.PlanID == currentPlan.ID &&
		summary.TaskPlan.Version == currentPlan.Version
}

func explorationCallSignature(tool string, arguments string) string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	normalized := map[string]any{
		"tool":      tool,
		"arguments": normalizeJSONForSignature(value),
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func normalizeJSONForSignature(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			if key == "timeout_ms" ||
				key == "flow_description" ||
				key == "core_user_flow_text" ||
				key == "observation_schema_version" ||
				key == "description" {
				continue
			}
			result[key] = normalizeJSONForSignature(nested)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, nested := range typed {
			result = append(result, normalizeJSONForSignature(nested))
		}
		return result
	case string:
		return normalizeSemanticText(typed)
	default:
		return typed
	}
}

func normalizeSemanticText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
