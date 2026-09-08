package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
	MaxExplorePageCalls                     int
	MaxExploreFlowCalls                     int
	MaxRepeatedExploreFlowSignatureCalls    int
	MinSuccessfulExplorationsForSufficiency int
	MinEvidenceItemsForSufficiency          int
}

type planStepStatus string

const (
	planStepCompleted planStepStatus = "completed"
	planStepFailed    planStepStatus = "failed"
)

type observedPlanStep struct {
	Key            string
	Status         planStepStatus
	SourceEventSeq int64
}

type explorationState struct {
	ExplorePageCalls                  int
	ExploreFlowCalls                  int
	SuccessfulExplorations            int
	VerifiedSelectors                 int
	SuccessfulActions                 int
	FailureCount                      int
	LatestGenerationNeedsMoreEvidence bool
	UnresolvedGenerationTargets       []string
	EvidenceItems                     map[string]struct{}
	Steps                             []observedPlanStep
	FlowSignatures                    map[string]int
}

var preflightMissingTargetPattern = regexp.MustCompile(`target ['"]([^'"]+)['"] matched 0 elements`)

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
	if c.MinSuccessfulExplorationsForSufficiency <= 0 {
		c.MinSuccessfulExplorationsForSufficiency = 3
	}
	if c.MinEvidenceItemsForSufficiency <= 0 {
		c.MinEvidenceItemsForSufficiency = 6
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
				"call generate_dsl with the retained a11y_nodes_by_state, or ask_user_question if the goal is impossible to ground",
			)
		}
	case "explore_flow":
		if state.ExploreFlowCalls >= config.MaxExploreFlowCalls {
			return explorationGateError(
				"explore_flow_budget_exhausted",
				state,
				"call generate_dsl with the retained a11y_nodes_by_state instead of probing again",
			)
		}
		signature := exploreFlowSignature(call.Arguments)
		if signature != "" &&
			state.FlowSignatures[signature] > config.MaxRepeatedExploreFlowSignatureCalls {
			return explorationGateError(
				"repeated_explore_flow_signature",
				state,
				"do not repeat the same probe; generate_dsl if evidence is enough, otherwise vary the semantic target once",
			)
		}
		if !state.LatestGenerationNeedsMoreEvidence &&
			state.sufficientForGeneration(config) {
			return explorationGateError(
				"facts_sufficient_for_generation",
				state,
				"stop exploring and call generate_dsl; later failures must be handled through report analysis and repair",
			)
		}
	}
	return nil
}

func buildExplorationState(transcript []agent.Message) explorationState {
	state := explorationState{
		EvidenceItems:  make(map[string]struct{}),
		FlowSignatures: make(map[string]int),
	}
	generationRepairEvidence := make(map[string]struct{})
	var latestGenerationMissingTargets []string
	trackGenerationRepair := false
	recordEvidence := func(parts ...string) {
		addEvidence(state.EvidenceItems, parts...)
		if trackGenerationRepair {
			addEvidence(generationRepairEvidence, parts...)
		}
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
		if missingTargets, ok := generateDSLFailureEvidenceTargets(message.Content); ok {
			latestGenerationMissingTargets = missingTargets
			generationRepairEvidence = make(map[string]struct{})
			trackGenerationRepair = true
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
		if isSuccessfulExploration(summary) {
			state.SuccessfulExplorations++
		}
		state.FailureCount += len(summary.Failures)
		for _, page := range summary.Pages {
			if page.Failure != nil {
				state.FailureCount++
				state.Steps = append(state.Steps, observedPlanStep{
					Key:    pageKey("page_failed", page.URL, page.PageState),
					Status: planStepFailed, SourceEventSeq: summary.Source.EventSeq,
				})
			}
			if !strings.EqualFold(page.Status, "error") {
				recordEvidence("url", page.URL)
				recordEvidence("page_state", page.PageState)
				recordEvidence("page_kind", page.PageKind)
				state.Steps = append(state.Steps, observedPlanStep{
					Key:    pageKey("page_observed", page.URL, page.PageState),
					Status: planStepCompleted, SourceEventSeq: summary.Source.EventSeq,
				})
				for _, node := range page.A11yNodes {
					recordEvidence("node", node.Role, node.Name)
					for _, selector := range node.VerifiedSelectors {
						state.VerifiedSelectors++
						recordEvidence(
							"selector",
							selector.Strategy,
							selector.Selector,
						)
					}
				}
			}
			for _, action := range page.Actions {
				if action.Failure != nil {
					state.FailureCount++
					state.Steps = append(state.Steps, observedPlanStep{
						Key:    pageKey("action_failed", action.Action, action.Target),
						Status: planStepFailed, SourceEventSeq: summary.Source.EventSeq,
					})
					continue
				}
				if strings.EqualFold(action.Status, "success") {
					state.SuccessfulActions++
					recordEvidence("action", action.Action, action.Target)
					state.Steps = append(state.Steps, observedPlanStep{
						Key:    pageKey("action_verified", action.Action, action.Target),
						Status: planStepCompleted, SourceEventSeq: summary.Source.EventSeq,
					})
				}
			}
		}
		if summary.Observation != nil {
			for _, candidate := range summary.Observation.CandidateCoverage {
				if candidate.Executable {
					recordEvidence(
						"candidate",
						candidate.Role,
						candidate.Label,
						candidate.PrimarySelector,
					)
				}
			}
			for _, option := range summary.Observation.ActionOptions {
				if strings.EqualFold(option.Status, "success") {
					state.SuccessfulActions++
					recordEvidence("action_option", option.Action, option.Target)
				}
			}
			for _, fact := range summary.Observation.VerificationFacts {
				recordEvidence("fact", fact.Kind, fact.Label, fact.Selector)
			}
		}
	}
	state.UnresolvedGenerationTargets = unresolvedEvidenceTargets(
		latestGenerationMissingTargets,
		generationRepairEvidence,
	)
	state.LatestGenerationNeedsMoreEvidence = len(state.UnresolvedGenerationTargets) > 0
	state.Steps = compactObservedPlanSteps(state.Steps)
	return state
}

func (s explorationState) sufficientForGeneration(config ExplorationGateConfig) bool {
	return s.SuccessfulExplorations >= config.MinSuccessfulExplorationsForSufficiency &&
		len(s.EvidenceItems) >= config.MinEvidenceItemsForSufficiency &&
		(s.VerifiedSelectors > 0 || s.SuccessfulActions > 0)
}

func explorationGateError(
	code string,
	state explorationState,
	nextAction string,
) error {
	return fmt.Errorf(
		"exploration gate %s: completed_plan_steps=%d successful_explorations=%d explore_page_calls=%d explore_flow_calls=%d evidence_items=%d verified_selectors=%d successful_actions=%d failures=%d unresolved_generation_targets=%q; %s",
		code,
		countStepsByStatus(state.Steps, planStepCompleted),
		state.SuccessfulExplorations,
		state.ExplorePageCalls,
		state.ExploreFlowCalls,
		len(state.EvidenceItems),
		state.VerifiedSelectors,
		state.SuccessfulActions,
		state.FailureCount,
		state.UnresolvedGenerationTargets,
		nextAction,
	)
}

func generateDSLFailureEvidenceTargets(content string) ([]string, bool) {
	var failure struct {
		Status  string `json:"status"`
		Tool    string `json:"tool"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(content), &failure) != nil ||
		failure.Status != "error" ||
		failure.Tool != "generate_dsl" {
		return nil, false
	}
	normalized := normalizeSemanticText(failure.Message)
	if !strings.Contains(normalized, "locator preflight failed") &&
		!strings.Contains(normalized, "matched 0 elements") &&
		!strings.Contains(normalized, "evidence") {
		return nil, false
	}
	matches := preflightMissingTargetPattern.FindAllStringSubmatch(failure.Message, -1)
	targets := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		target := normalizeSemanticText(match[1])
		if target == "" {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		targets = []string{"evidence"}
	}
	return targets, true
}

func unresolvedEvidenceTargets(targets []string, evidence map[string]struct{}) []string {
	unresolved := make([]string, 0, len(targets))
	for _, target := range targets {
		if target == "" || evidenceContainsTarget(evidence, target) {
			continue
		}
		unresolved = append(unresolved, target)
	}
	return unresolved
}

func evidenceContainsTarget(evidence map[string]struct{}, target string) bool {
	target = normalizeSemanticText(target)
	if target == "" {
		return true
	}
	for item := range evidence {
		normalizedItem := strings.ReplaceAll(item, "\x00", " ")
		if strings.Contains(normalizedItem, target) {
			return true
		}
	}
	return false
}

func countStepsByStatus(steps []observedPlanStep, status planStepStatus) int {
	count := 0
	for _, step := range steps {
		if step.Status == status {
			count++
		}
	}
	return count
}

func isSuccessfulExploration(summary agent.ModelToolSummary) bool {
	if summary.Success != nil && !*summary.Success {
		return false
	}
	if strings.EqualFold(summary.Status, "error") {
		return false
	}
	for _, page := range summary.Pages {
		if !strings.EqualFold(page.Status, "error") && page.Failure == nil {
			return true
		}
	}
	return false
}

func compactObservedPlanSteps(steps []observedPlanStep) []observedPlanStep {
	byKey := make(map[string]observedPlanStep)
	for _, step := range steps {
		existing, ok := byKey[step.Key]
		if !ok || step.SourceEventSeq > existing.SourceEventSeq {
			byKey[step.Key] = step
		}
	}
	result := make([]observedPlanStep, 0, len(byKey))
	for _, step := range byKey {
		result = append(result, step)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

func addEvidence(items map[string]struct{}, parts ...string) {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		value := normalizeSemanticText(part)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 {
		return
	}
	items[strings.Join(normalized, "\x00")] = struct{}{}
}

func pageKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, normalizeSemanticText(part))
	}
	return strings.Join(normalized, "\x00")
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
