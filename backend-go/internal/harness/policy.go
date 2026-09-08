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
)

type ToolPolicy interface {
	BeforeToolCall(run agentservice.AgentRun, call agent.ModelTool) error
}

type DefaultToolPolicy struct {
	Exploration ExplorationGateConfig
}

type ExplorationGateConfig struct {
	MaxExplorePageCalls                  int
	MaxExploreFlowCalls                  int
	MaxRepeatedExploreFlowSignatureCalls int
}

type explorationState struct {
	ExplorePageCalls int
	ExploreFlowCalls int
	FlowSignatures   map[string]int
}

func (c ExplorationGateConfig) withDefaults() ExplorationGateConfig {
	if c.MaxExplorePageCalls <= 0 {
		c.MaxExplorePageCalls = 5
	}
	if c.MaxExploreFlowCalls <= 0 {
		c.MaxExploreFlowCalls = 4
	}
	if c.MaxRepeatedExploreFlowSignatureCalls <= 0 {
		c.MaxRepeatedExploreFlowSignatureCalls = 1
	}
	return c
}

func (p DefaultToolPolicy) BeforeToolCall(run agentservice.AgentRun, call agent.ModelTool) error {
	switch call.Name {
	case "execute_dsl":
		if run.ApprovedGenerationID == nil {
			return errors.New("execute_dsl requires an approved DSL generation")
		}
	case "explore_page", "explore_flow":
		return p.beforeExplorationCall(run, call)
	}
	return nil
}

func (p DefaultToolPolicy) beforeExplorationCall(
	run agentservice.AgentRun,
	call agent.ModelTool,
) error {
	config := p.Exploration.withDefaults()
	state := buildExplorationState(run.Transcript)
	switch call.Name {
	case "explore_page":
		if state.ExplorePageCalls >= config.MaxExplorePageCalls {
			return explorationGateError(
				"explore_page_budget_exhausted",
				state,
				"stop probing; use the persisted TaskPlan state to generate only if all steps are grounded, otherwise ask the user or revise the plan",
			)
		}
	case "explore_flow":
		if state.ExploreFlowCalls >= config.MaxExploreFlowCalls {
			return explorationGateError(
				"explore_flow_budget_exhausted",
				state,
				"stop probing; generate only when the persisted TaskPlan is ready, otherwise ask the user or revise the plan",
			)
		}
		signature := exploreFlowSignature(call.Arguments)
		if signature != "" &&
			state.FlowSignatures[signature] > config.MaxRepeatedExploreFlowSignatureCalls {
			return explorationGateError(
				"repeated_explore_flow_signature",
				state,
				"do not repeat the same probe; follow the persisted TaskPlan state",
			)
		}
	}
	return nil
}

func buildExplorationState(transcript []agent.Message) explorationState {
	state := explorationState{
		FlowSignatures: make(map[string]int),
	}
	for _, message := range transcript {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				if call.Name == "explore_flow" {
					if signature := exploreFlowSignature(call.Arguments); signature != "" {
						state.FlowSignatures[signature]++
					}
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
			state.ExplorePageCalls++
		} else {
			state.ExploreFlowCalls++
		}
	}
	return state
}

func explorationGateError(
	code string,
	state explorationState,
	nextAction string,
) error {
	return fmt.Errorf(
		"exploration gate %s: explore_page_calls=%d explore_flow_calls=%d; %s",
		code,
		state.ExplorePageCalls,
		state.ExploreFlowCalls,
		nextAction,
	)
}

func exploreFlowSignature(arguments string) string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return ""
	}
	normalized := normalizeJSONForSignature(value)
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
			if key == "timeout_ms" {
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
