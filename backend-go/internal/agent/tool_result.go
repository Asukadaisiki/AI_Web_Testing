package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	ModelToolSummaryTargetBytes    = 32 << 10
	ModelToolSummaryHardLimitBytes = 64 << 10
	ModelExplorationBudgetBytes    = 160 << 10
)

type ToolResultEventPayload struct {
	SchemaVersion string          `json:"schema_version"`
	Tool          string          `json:"tool"`
	Content       json.RawMessage `json:"content"`
	ContentSHA256 string          `json:"content_sha256"`
	ContentBytes  int             `json:"content_bytes"`
}

type ToolResultSource struct {
	EventSeq      int64  `json:"event_seq"`
	ContentSHA256 string `json:"content_sha256"`
	ContentBytes  int    `json:"content_bytes"`
}

type ToolResultOmissionCounters struct {
	Pages          int `json:"pages,omitempty"`
	Actions        int `json:"actions,omitempty"`
	Nodes          int `json:"nodes,omitempty"`
	TargetEvidence int `json:"target_evidence,omitempty"`
	Selectors      int `json:"selectors,omitempty"`
}

type ToolResultTruncation struct {
	Truncated    bool                       `json:"truncated"`
	Reason       string                     `json:"reason,omitempty"`
	TargetBytes  int                        `json:"target_bytes"`
	HardLimit    int                        `json:"hard_limit_bytes"`
	SummaryBytes int                        `json:"summary_bytes"`
	Omitted      ToolResultOmissionCounters `json:"omitted"`
}

type ToolResultErrorSummary struct {
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	StepIndex   *int   `json:"step_index,omitempty"`
	ActionIndex *int   `json:"action_index,omitempty"`
	Action      string `json:"action,omitempty"`
	Target      string `json:"target,omitempty"`
}

type ToolResultSelectorSummary struct {
	Strategy string `json:"strategy,omitempty"`
	Selector string `json:"selector"`
	Name     string `json:"name,omitempty"`
	Source   string `json:"source,omitempty"`
}

type ToolResultNodeSummary struct {
	NodeID            string                      `json:"node_id,omitempty"`
	BackendDOMNodeID  json.RawMessage             `json:"backend_dom_node_id,omitempty"`
	ParentID          string                      `json:"parent_id,omitempty"`
	Role              string                      `json:"role,omitempty"`
	Name              string                      `json:"name,omitempty"`
	PageState         string                      `json:"page_state,omitempty"`
	Source            string                      `json:"source,omitempty"`
	Focusable         bool                        `json:"focusable,omitempty"`
	Disabled          bool                        `json:"disabled,omitempty"`
	DOM               *ToolResultDOMSummary       `json:"dom,omitempty"`
	VerifiedSelectors []ToolResultSelectorSummary `json:"verified_selectors,omitempty"`
}

type ToolResultDOMSummary struct {
	Tag   string            `json:"tag,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

type ToolResultActionSummary struct {
	StepIndex        *int                    `json:"step_index,omitempty"`
	ActionIndex      *int                    `json:"action_index,omitempty"`
	Action           string                  `json:"action,omitempty"`
	Target           string                  `json:"target,omitempty"`
	Description      string                  `json:"description,omitempty"`
	Phase            string                  `json:"phase,omitempty"`
	Status           string                  `json:"status,omitempty"`
	URL              string                  `json:"url,omitempty"`
	PageState        string                  `json:"page_state,omitempty"`
	TargetEvidence   []ToolResultNodeSummary `json:"target_evidence,omitempty"`
	EvidenceCount    int                     `json:"evidence_count"`
	OmittedEvidence  int                     `json:"omitted_target_evidence,omitempty"`
	OmittedSelectors int                     `json:"omitted_selectors,omitempty"`
	Failure          *ToolResultErrorSummary `json:"failure,omitempty"`
}

type ToolResultPageSummary struct {
	URL           string                     `json:"url,omitempty"`
	PageState     string                     `json:"page_state,omitempty"`
	PageKind      string                     `json:"page_kind,omitempty"`
	Revision      int                        `json:"revision,omitempty"`
	Status        string                     `json:"status,omitempty"`
	Description   string                     `json:"description,omitempty"`
	ElementCount  int                        `json:"element_count"`
	Actions       []ToolResultActionSummary  `json:"actions,omitempty"`
	A11yNodes     []ToolResultNodeSummary    `json:"a11y_nodes,omitempty"`
	Failure       *ToolResultErrorSummary    `json:"failure,omitempty"`
	Omitted       ToolResultOmissionCounters `json:"omitted"`
	ReferenceOnly bool                       `json:"reference_only,omitempty"`
}

type ModelToolSummary struct {
	SchemaVersion   string                      `json:"schema_version"`
	PolicyVersion   string                      `json:"policy_version"`
	Tool            string                      `json:"tool"`
	Source          ToolResultSource            `json:"source"`
	SummarySHA256   string                      `json:"summary_sha256,omitempty"`
	Success         *bool                       `json:"success,omitempty"`
	Status          string                      `json:"status,omitempty"`
	Warnings        []string                    `json:"warnings,omitempty"`
	Failures        []ToolResultErrorSummary    `json:"failures,omitempty"`
	Context         *ToolResultContextSummary   `json:"context,omitempty"`
	Observation     *StructuredObservation      `json:"observation,omitempty"`
	ExecutedEffects []ObservedActionOption      `json:"executed_effects,omitempty"`
	TaskPlan        *ToolResultTaskPlanSummary  `json:"task_plan,omitempty"`
	DSL             *ToolResultDSLSummary       `json:"dsl,omitempty"`
	Execution       *ToolResultExecutionSummary `json:"execution,omitempty"`
	Report          *ToolResultReportSummary    `json:"report,omitempty"`
	Repair          *ToolResultRepairSummary    `json:"repair,omitempty"`
	Generic         *ToolResultGenericSummary   `json:"generic,omitempty"`
	Pages           []ToolResultPageSummary     `json:"pages,omitempty"`
	Truncation      ToolResultTruncation        `json:"truncation"`
	ReferenceOnly   bool                        `json:"reference_only,omitempty"`
}

type ToolResultContextSummary struct {
	ExecutionScope        string `json:"execution_scope,omitempty"`
	StatePersisted        bool   `json:"state_persisted"`
	CleanContextRequested bool   `json:"clean_context_requested"`
	StorageStateLoaded    bool   `json:"storage_state_loaded"`
	PlanningSessionID     int64  `json:"planning_session_id,omitempty"`
}

type StructuredObservation struct {
	SchemaVersion     string                      `json:"schema_version"`
	PageStates        []ObservedPageState         `json:"page_states,omitempty"`
	ElementGroups     []ObservedElementGroup      `json:"element_groups,omitempty"`
	CandidateCoverage []ObservedCandidateCoverage `json:"candidate_coverage,omitempty"`
	ActionOptions     []ObservedActionOption      `json:"action_options,omitempty"`
	VerificationFacts []ObservedVerificationFact  `json:"verification_facts,omitempty"`
	RecoveryHints     []ObservedRecoveryHint      `json:"recovery_hints,omitempty"`
}

type ObservedPageState struct {
	PageState    string `json:"page_state,omitempty"`
	PageKind     string `json:"page_kind,omitempty"`
	URL          string `json:"url,omitempty"`
	Status       string `json:"status,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	ElementCount int    `json:"element_count"`
}

type ObservedElementGroup struct {
	PageState      string   `json:"page_state,omitempty"`
	Category       string   `json:"category"`
	Count          int      `json:"count"`
	VerifiedCount  int      `json:"verified_count,omitempty"`
	Representative []string `json:"representative,omitempty"`
}

type ObservedCandidateCoverage struct {
	PageState       string `json:"page_state,omitempty"`
	Category        string `json:"category,omitempty"`
	Label           string `json:"label,omitempty"`
	Role            string `json:"role,omitempty"`
	SelectorCount   int    `json:"selector_count"`
	PrimarySelector string `json:"primary_selector,omitempty"`
	Source          string `json:"source,omitempty"`
	Executable      bool   `json:"executable"`
	AmbiguousCount  int    `json:"ambiguous_count,omitempty"`
}

type ObservedActionOption struct {
	PageState       string `json:"page_state,omitempty"`
	URL             string `json:"url,omitempty"`
	Action          string `json:"action,omitempty"`
	Target          string `json:"target,omitempty"`
	Status          string `json:"status,omitempty"`
	EvidenceCount   int    `json:"evidence_count,omitempty"`
	SideEffect      string `json:"side_effect,omitempty"`
	IdempotencyHint string `json:"idempotency_hint,omitempty"`
}

type ObservedVerificationFact struct {
	PageState string `json:"page_state,omitempty"`
	Kind      string `json:"kind"`
	Label     string `json:"label,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Source    string `json:"source,omitempty"`
}

type ObservedRecoveryHint struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Action   string `json:"action"`
}

type ToolResultDSLSummary struct {
	GenerationID any      `json:"generation_id,omitempty"`
	Profile      string   `json:"profile,omitempty"`
	CaseName     string   `json:"case_name,omitempty"`
	StepCount    int      `json:"step_count"`
	Actions      []string `json:"actions,omitempty"`
	Targets      []string `json:"targets,omitempty"`
	PlanID       string   `json:"plan_id,omitempty"`
	PlanVersion  int      `json:"plan_version,omitempty"`
	PlanSHA256   string   `json:"plan_sha256,omitempty"`
}

type ToolResultTaskPlanSummary struct {
	PlanID     string   `json:"plan_id,omitempty"`
	Version    int      `json:"version,omitempty"`
	PlanSHA256 string   `json:"plan_sha256,omitempty"`
	Status     string   `json:"status,omitempty"`
	StepIDs    []string `json:"step_ids,omitempty"`
}

type ToolResultExecutionSummary struct {
	BatchID      any    `json:"batch_id,omitempty"`
	CaseID       any    `json:"case_id,omitempty"`
	Status       string `json:"status,omitempty"`
	ReportAPIURL string `json:"report_api_url,omitempty"`
}

type ToolResultReportSummary struct {
	BatchID           any                      `json:"batch_id,omitempty"`
	Status            string                   `json:"status,omitempty"`
	CaseResults       []ToolResultCaseResult   `json:"case_results,omitempty"`
	FailureSignals    []ToolResultFailureBrief `json:"failure_signals,omitempty"`
	RecommendedAction string                   `json:"recommended_action,omitempty"`
}

type ToolResultCaseResult struct {
	CaseID         any    `json:"case_id,omitempty"`
	CaseName       string `json:"case_name,omitempty"`
	Status         string `json:"status,omitempty"`
	PassedSteps    int    `json:"passed_steps,omitempty"`
	TotalSteps     int    `json:"total_steps,omitempty"`
	FailureSummary string `json:"failure_summary,omitempty"`
}

type ToolResultFailureBrief struct {
	Category            string `json:"category,omitempty"`
	Stage               string `json:"stage,omitempty"`
	Code                string `json:"code,omitempty"`
	Title               string `json:"title,omitempty"`
	Retryable           *bool  `json:"retryable,omitempty"`
	SideEffectCommitted any    `json:"side_effect_committed,omitempty"`
}

type ToolResultRepairSummary struct {
	Status                      string                   `json:"status,omitempty"`
	Strategy                    string                   `json:"strategy,omitempty"`
	Reason                      string                   `json:"reason,omitempty"`
	SourceBatchID               any                      `json:"source_batch_id,omitempty"`
	SourceExecutionID           any                      `json:"source_execution_id,omitempty"`
	OriginalActionReplayAllowed *bool                    `json:"original_action_replay_allowed,omitempty"`
	FailureSignals              []ToolResultFailureBrief `json:"failure_signals,omitempty"`
}

type ToolResultGenericSummary struct {
	Status       string   `json:"status,omitempty"`
	TopLevelKeys []string `json:"top_level_keys,omitempty"`
}

type rawExploreResult struct {
	URL             string              `json:"url"`
	PageState       string              `json:"page_state"`
	Revision        int                 `json:"revision"`
	Status          string              `json:"status"`
	Warning         string              `json:"warning"`
	Success         *bool               `json:"success"`
	ElementCount    int                 `json:"element_count"`
	A11yNodes       []rawNode           `json:"a11y_nodes"`
	Actions         []rawAction         `json:"actions"`
	Failure         *rawFailure         `json:"failure"`
	Failures        []rawFailure        `json:"failures"`
	Pages           []rawPage           `json:"pages"`
	ContextEvidence *rawContextEvidence `json:"context_evidence"`
}

type rawContextEvidence struct {
	ExecutionScope        string `json:"execution_scope"`
	StatePersisted        bool   `json:"state_persisted"`
	CleanContextRequested bool   `json:"clean_context_requested"`
	StorageStateLoaded    bool   `json:"storage_state_loaded"`
	PlanningSessionID     int64  `json:"planning_session_id"`
}

type rawPage struct {
	URL          string      `json:"url"`
	PageState    string      `json:"page_state"`
	Revision     int         `json:"revision"`
	Status       string      `json:"status"`
	Description  string      `json:"description"`
	ElementCount int         `json:"element_count"`
	A11yNodes    []rawNode   `json:"a11y_nodes"`
	Actions      []rawAction `json:"actions"`
	Failure      *rawFailure `json:"failure"`
}

type rawAction struct {
	StepIndex         *int        `json:"step_index"`
	ActionIndex       *int        `json:"action_index"`
	Action            string      `json:"action"`
	Target            string      `json:"target"`
	ActionDescription string      `json:"action_description"`
	Phase             string      `json:"phase"`
	Status            string      `json:"status"`
	URL               string      `json:"url"`
	PageState         string      `json:"page_state"`
	EvidenceCount     int         `json:"evidence_count"`
	TargetEvidence    []rawNode   `json:"target_evidence"`
	Failure           *rawFailure `json:"failure"`
}

type rawNode struct {
	NodeID           string          `json:"node_id"`
	BackendDOMNodeID json.RawMessage `json:"backend_dom_node_id"`
	ParentID         *string         `json:"parent_id"`
	Role             string          `json:"role"`
	Name             string          `json:"name"`
	PageState        string          `json:"page_state"`
	Source           string          `json:"source"`
	Focusable        bool            `json:"focusable"`
	Disabled         bool            `json:"disabled"`
	DOM              struct {
		Tag   string            `json:"tag"`
		Attrs map[string]string `json:"attrs"`
	} `json:"dom"`
	VerifiedSelectors []ToolResultSelectorSummary `json:"verified_selectors"`
}

type rawFailure struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	StepIndex   *int   `json:"step_index"`
	ActionIndex *int   `json:"action_index"`
	Action      string `json:"action"`
	Target      string `json:"target"`
}

func NewToolResultEventPayload(tool string, content json.RawMessage) (ToolResultEventPayload, error) {
	if len(content) == 0 || !json.Valid(content) || !utf8.Valid(content) {
		return ToolResultEventPayload{}, errors.New("tool result content must be valid UTF-8 JSON")
	}
	return ToolResultEventPayload{
		SchemaVersion: ToolResultSchemaV1,
		Tool:          tool,
		Content:       content,
		ContentSHA256: sha256HexBytes(content),
		ContentBytes:  len(content),
	}, nil
}

func IsExplorationTool(tool string) bool {
	return tool == "explore_page" || tool == "explore_flow"
}

func BuildModelToolSummary(
	tool string,
	content json.RawMessage,
	sourceEventSeq int64,
) (string, error) {
	payload, err := NewToolResultEventPayload(tool, content)
	if err != nil {
		return "", err
	}
	if !IsExplorationTool(tool) {
		return buildCapabilityToolSummary(tool, content, payload, sourceEventSeq)
	}
	var raw rawExploreResult
	if err := json.Unmarshal(content, &raw); err != nil {
		return "", fmt.Errorf("decode exploration result: %w", err)
	}
	summary := ModelToolSummary{
		SchemaVersion: ModelToolSummarySchemaV1,
		PolicyVersion: ToolSummaryPolicyV1,
		Tool:          tool,
		Source: ToolResultSource{
			EventSeq:      sourceEventSeq,
			ContentSHA256: payload.ContentSHA256,
			ContentBytes:  payload.ContentBytes,
		},
		Success: raw.Success,
		Status:  boundedUTF8(raw.Status, 64),
		Truncation: ToolResultTruncation{
			TargetBytes: ModelToolSummaryTargetBytes,
			HardLimit:   ModelToolSummaryHardLimitBytes,
		},
	}
	if raw.ContextEvidence != nil {
		summary.Context = &ToolResultContextSummary{
			ExecutionScope:        boundedUTF8(raw.ContextEvidence.ExecutionScope, 64),
			StatePersisted:        raw.ContextEvidence.StatePersisted,
			CleanContextRequested: raw.ContextEvidence.CleanContextRequested,
			StorageStateLoaded:    raw.ContextEvidence.StorageStateLoaded,
			PlanningSessionID:     raw.ContextEvidence.PlanningSessionID,
		}
	}
	if raw.Warning != "" {
		summary.Warnings = []string{boundedUTF8(raw.Warning, 1024)}
	}
	for _, failure := range raw.Failures {
		summary.Failures = append(summary.Failures, *summarizeFailure(&failure))
	}
	if raw.Failure != nil {
		summary.Failures = append(summary.Failures, *summarizeFailure(raw.Failure))
	}
	if tool == "explore_page" {
		summary.Pages = []ToolResultPageSummary{summarizePage(rawPage{
			URL: raw.URL, PageState: firstNonEmptyString(raw.PageState, "S0"),
			Revision: raw.Revision, Status: raw.Status,
			ElementCount: raw.ElementCount, A11yNodes: raw.A11yNodes,
			Actions: raw.Actions, Failure: raw.Failure,
		})}
	} else {
		for _, page := range raw.Pages {
			summary.Pages = append(summary.Pages, summarizePage(page))
		}
	}
	normalizeSummary(&summary)
	summary.Observation = buildStructuredObservation(summary.Pages, summary.Failures)
	summary.ExecutedEffects = executedEffectsFromObservation(summary.Observation)
	return encodeBoundedSummary(&summary)
}

func executedEffectsFromObservation(
	observation *StructuredObservation,
) []ObservedActionOption {
	if observation == nil {
		return nil
	}
	seen := make(map[string]ObservedActionOption)
	for _, option := range observation.ActionOptions {
		if option.Status != "success" ||
			(option.SideEffect != "external_or_business_state" &&
				option.IdempotencyHint != "non_idempotent") {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(option.Action)) +
			"\x00" + strings.ToLower(strings.Join(strings.Fields(option.Target), " ")) +
			"\x00" + strings.ToLower(strings.TrimSpace(option.URL))
		if key == "\x00\x00" {
			continue
		}
		seen[key] = option
	}
	result := make([]ObservedActionOption, 0, len(seen))
	for _, option := range seen {
		result = append(result, option)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Action != result[j].Action {
			return result[i].Action < result[j].Action
		}
		if result[i].Target != result[j].Target {
			return result[i].Target < result[j].Target
		}
		return result[i].URL < result[j].URL
	})
	return result
}

func CompactExplorationTranscript(transcript []Message) []Message {
	total := explorationSummaryBytes(transcript)
	if total <= ModelExplorationBudgetBytes {
		return transcript
	}
	type locatedSummary struct {
		index   int
		summary ModelToolSummary
	}
	located := make([]locatedSummary, 0)
	latest := make(map[string]int64)
	for index, message := range transcript {
		summary, ok := DecodeModelToolSummary(message.Content)
		if !ok || message.Role != "tool" {
			continue
		}
		located = append(located, locatedSummary{index: index, summary: summary})
		for _, page := range summary.Pages {
			key := page.URL + "\x00" + page.PageState
			if summary.Source.EventSeq > latest[key] {
				latest[key] = summary.Source.EventSeq
			}
		}
	}
	for locationIndex := range located {
		if total <= ModelExplorationBudgetBytes {
			break
		}
		item := &located[locationIndex]
		changed := false
		for pageIndex := range item.summary.Pages {
			page := &item.summary.Pages[pageIndex]
			key := page.URL + "\x00" + page.PageState
			if item.summary.Source.EventSeq >= latest[key] || page.ReferenceOnly {
				continue
			}
			addOmissions(
				&item.summary.Truncation.Omitted,
				makePageReferenceOnly(page),
			)
			item.summary.Truncation.Truncated = true
			item.summary.Truncation.Reason = "superseded_revision"
			changed = true
		}
		if changed {
			item.summary.ReferenceOnly = true
			for _, page := range item.summary.Pages {
				if !page.ReferenceOnly {
					item.summary.ReferenceOnly = false
					break
				}
			}
			encoded, err := encodeSummary(&item.summary)
			if err == nil {
				total -= len(transcript[item.index].Content)
				transcript[item.index].Content = string(encoded)
				total += len(encoded)
			}
		}
	}
	for locationIndex := range located {
		if total <= ModelExplorationBudgetBytes {
			break
		}
		item := &located[locationIndex]
		if item.summary.ReferenceOnly {
			continue
		}
		makeSummaryReferenceOnly(&item.summary, "aggregate_budget")
		encoded, err := encodeSummary(&item.summary)
		if err != nil {
			continue
		}
		total -= len(transcript[item.index].Content)
		transcript[item.index].Content = string(encoded)
		total += len(encoded)
	}
	return transcript
}

func explorationSummaryBytes(messages []Message) int {
	total := 0
	for _, message := range messages {
		if summary, ok := DecodeModelToolSummary(message.Content); ok &&
			IsExplorationTool(summary.Tool) {
			total += len(message.Content)
		}
	}
	return total
}

func DecodeModelToolSummary(content string) (ModelToolSummary, bool) {
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal([]byte(content), &envelope) != nil ||
		envelope.SchemaVersion != ModelToolSummarySchemaV1 {
		return ModelToolSummary{}, false
	}
	var summary ModelToolSummary
	if json.Unmarshal([]byte(content), &summary) != nil {
		return ModelToolSummary{}, false
	}
	return summary, true
}

func summarizePage(page rawPage) ToolResultPageSummary {
	result := ToolResultPageSummary{
		URL: boundedUTF8(page.URL, 2048), PageState: boundedUTF8(page.PageState, 256),
		PageKind: classifyPageKind(page.URL, page.PageState, page.Description),
		Revision: page.Revision, Status: boundedUTF8(firstNonEmptyString(page.Status, "success"), 64),
		Description: boundedUTF8(page.Description, 512), ElementCount: page.ElementCount,
		Failure: summarizeFailure(page.Failure),
	}
	nodesByID := make(map[string]rawNode, len(page.A11yNodes))
	selectedIDs := make(map[string]bool)
	for _, node := range page.A11yNodes {
		nodesByID[node.NodeID] = node
		if node.Focusable || isInteractiveRole(node.Role) || len(node.VerifiedSelectors) > 0 ||
			(strings.TrimSpace(node.Name) != "" && isSemanticRole(node.Role)) {
			selectedIDs[node.NodeID] = true
		}
	}
	for nodeID := range selectedIDs {
		current := nodesByID[nodeID]
		for current.ParentID != nil && *current.ParentID != "" {
			parentID := *current.ParentID
			if selectedIDs[parentID] {
				break
			}
			parent, ok := nodesByID[parentID]
			if !ok {
				break
			}
			selectedIDs[parentID] = true
			current = parent
		}
	}
	for _, node := range page.A11yNodes {
		if selectedIDs[node.NodeID] {
			summarized, omittedSelectors := summarizeNode(node)
			result.A11yNodes = append(result.A11yNodes, summarized)
			result.Omitted.Selectors += omittedSelectors
		}
	}
	result.A11yNodes = deduplicateNodes(result.A11yNodes)
	result.Omitted.Nodes = len(page.A11yNodes) - len(result.A11yNodes)
	for _, action := range page.Actions {
		summarized := summarizeAction(action)
		result.Omitted.TargetEvidence += summarized.OmittedEvidence
		result.Omitted.Selectors += summarized.OmittedSelectors
		result.Actions = append(result.Actions, summarized)
	}
	return result
}

func buildCapabilityToolSummary(
	tool string,
	content json.RawMessage,
	payload ToolResultEventPayload,
	sourceEventSeq int64,
) (string, error) {
	summary := ModelToolSummary{
		SchemaVersion: ModelToolSummarySchemaV1,
		PolicyVersion: ToolSummaryPolicyV1,
		Tool:          tool,
		Source: ToolResultSource{
			EventSeq:      sourceEventSeq,
			ContentSHA256: payload.ContentSHA256,
			ContentBytes:  payload.ContentBytes,
		},
		Truncation: ToolResultTruncation{
			TargetBytes: ModelToolSummaryTargetBytes,
			HardLimit:   ModelToolSummaryHardLimitBytes,
		},
	}
	var value map[string]any
	if err := json.Unmarshal(content, &value); err != nil {
		return "", fmt.Errorf("decode tool result: %w", err)
	}
	summary.Status = boundedUTF8(stringValue(value["status"]), 64)
	switch tool {
	case "set_task_plan":
		summary.TaskPlan = summarizeTaskPlanResult(value)
	case "generate_dsl":
		summary.DSL = summarizeDSLResult(value)
	case "execute_dsl":
		summary.Execution = summarizeExecutionResult(value)
	case "get_report":
		summary.Report = summarizeReportResult(value)
	case "fix_and_retry":
		summary.Repair = summarizeRepairResult(value)
	default:
		summary.Generic = summarizeGenericResult(value)
	}
	return encodeBoundedSummary(&summary)
}

func summarizeTaskPlanResult(value map[string]any) *ToolResultTaskPlanSummary {
	result := &ToolResultTaskPlanSummary{
		PlanID:     boundedUTF8(stringValue(value["plan_id"]), 64),
		Version:    intFromAny(value["version"]),
		PlanSHA256: boundedUTF8(stringValue(value["plan_sha256"]), 64),
		Status:     boundedUTF8(stringValue(value["status"]), 64),
	}
	for _, rawStep := range arrayValue(value["steps"]) {
		step, _ := rawStep.(map[string]any)
		if step == nil {
			continue
		}
		if id := boundedUTF8(stringValue(step["id"]), 64); id != "" {
			result.StepIDs = append(result.StepIDs, id)
		}
	}
	return result
}

func summarizeDSLResult(value map[string]any) *ToolResultDSLSummary {
	result := &ToolResultDSLSummary{
		GenerationID: scalarValue(value["generation_id"]),
	}
	if binding, _ := value["plan_binding"].(map[string]any); binding != nil {
		result.PlanID = boundedUTF8(stringValue(binding["plan_id"]), 64)
		result.PlanVersion = intFromAny(binding["version"])
		result.PlanSHA256 = boundedUTF8(
			stringValue(binding["sha256"]),
			64,
		)
	}
	caseValue, _ := value["case"].(map[string]any)
	if caseValue == nil {
		return result
	}
	result.Profile = boundedUTF8(stringValue(caseValue["profile"]), 64)
	result.CaseName = boundedUTF8(stringValue(caseValue["name"]), 256)
	steps, _ := caseValue["steps"].([]any)
	result.StepCount = len(steps)
	actionSet := make(map[string]bool)
	targetSet := make(map[string]bool)
	for _, rawStep := range steps {
		step, _ := rawStep.(map[string]any)
		if step == nil {
			continue
		}
		if action := boundedUTF8(stringValue(step["action"]), 64); action != "" {
			actionSet[action] = true
		}
		if target := boundedUTF8(stringValue(step["target"]), 256); target != "" {
			targetSet[target] = true
		}
	}
	result.Actions = sortedKeys(actionSet, 16)
	result.Targets = sortedKeys(targetSet, 24)
	return result
}

func summarizeExecutionResult(value map[string]any) *ToolResultExecutionSummary {
	return &ToolResultExecutionSummary{
		BatchID:      scalarValue(value["batch_id"]),
		CaseID:       scalarValue(value["case_id"]),
		Status:       boundedUTF8(stringValue(value["status"]), 64),
		ReportAPIURL: boundedUTF8(stringValue(value["report_api_url"]), 256),
	}
}

func summarizeReportResult(value map[string]any) *ToolResultReportSummary {
	result := &ToolResultReportSummary{
		BatchID: scalarValue(firstPresent(value, "id", "batch_id")),
		Status:  boundedUTF8(stringValue(value["status"]), 64),
	}
	if analysis, _ := value["analysis"].(map[string]any); analysis != nil {
		result.RecommendedAction = boundedUTF8(
			stringValue(analysis["recommended_action"]), 128,
		)
		for _, rawCase := range arrayValue(analysis["case_results"]) {
			caseResult, _ := rawCase.(map[string]any)
			if caseResult == nil {
				continue
			}
			result.CaseResults = append(result.CaseResults, ToolResultCaseResult{
				CaseID:         scalarValue(caseResult["case_id"]),
				CaseName:       boundedUTF8(stringValue(caseResult["case_name"]), 256),
				Status:         boundedUTF8(stringValue(caseResult["status"]), 64),
				PassedSteps:    intFromAny(caseResult["passed_steps"]),
				TotalSteps:     intFromAny(caseResult["total_steps"]),
				FailureSummary: boundedUTF8(stringValue(caseResult["failure_summary"]), 512),
			})
		}
		result.FailureSignals = append(
			result.FailureSignals,
			summarizeFailureSignals(arrayValue(analysis["failure_signals"]))...,
		)
	}
	result.FailureSignals = append(
		result.FailureSignals,
		summarizeFailureSignals(arrayValue(value["failure_signals"]))...,
	)
	result.FailureSignals = deduplicateFailureBriefs(result.FailureSignals)
	return result
}

func summarizeRepairResult(value map[string]any) *ToolResultRepairSummary {
	result := &ToolResultRepairSummary{
		Status:            boundedUTF8(stringValue(value["status"]), 64),
		Strategy:          boundedUTF8(stringValue(value["strategy"]), 64),
		Reason:            boundedUTF8(stringValue(value["reason"]), 512),
		SourceBatchID:     scalarValue(value["source_batch_id"]),
		SourceExecutionID: scalarValue(value["source_execution_id"]),
		FailureSignals:    summarizeFailureSignals(arrayValue(value["failure_signals"])),
	}
	if replay, ok := boolValue(value["original_action_replay_allowed"]); ok {
		result.OriginalActionReplayAllowed = &replay
	}
	return result
}

func summarizeGenericResult(value map[string]any) *ToolResultGenericSummary {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, boundedUTF8(key, 128))
	}
	sort.Strings(keys)
	if len(keys) > 32 {
		keys = keys[:32]
	}
	return &ToolResultGenericSummary{
		Status:       boundedUTF8(stringValue(value["status"]), 64),
		TopLevelKeys: keys,
	}
}

func summarizeFailureSignals(values []any) []ToolResultFailureBrief {
	result := make([]ToolResultFailureBrief, 0, len(values))
	for _, raw := range values {
		signal, _ := raw.(map[string]any)
		if signal == nil {
			continue
		}
		brief := ToolResultFailureBrief{
			Category:            boundedUTF8(stringValue(signal["category"]), 64),
			Stage:               boundedUTF8(stringValue(signal["stage"]), 64),
			Code:                boundedUTF8(stringValue(signal["code"]), 128),
			Title:               boundedUTF8(stringValue(signal["title"]), 256),
			SideEffectCommitted: scalarValue(signal["side_effect_committed"]),
		}
		if retryable, ok := boolValue(signal["retryable"]); ok {
			brief.Retryable = &retryable
		}
		result = append(result, brief)
	}
	return deduplicateFailureBriefs(result)
}

func summarizeAction(action rawAction) ToolResultActionSummary {
	actionName := boundedUTF8(action.Action, 64)
	target := boundedUTF8(action.Target, 1024)
	if actionName == "" || target == "" {
		parts := strings.SplitN(strings.TrimSpace(action.ActionDescription), " ", 2)
		if actionName == "" && len(parts) > 0 {
			actionName = boundedUTF8(parts[0], 64)
		}
		if target == "" && len(parts) == 2 {
			target = boundedUTF8(parts[1], 1024)
		}
	}
	result := ToolResultActionSummary{
		StepIndex: action.StepIndex, ActionIndex: action.ActionIndex,
		Action: actionName, Target: target,
		Description: boundedUTF8(action.ActionDescription, 1200),
		Phase:       boundedUTF8(action.Phase, 64), Status: boundedUTF8(action.Status, 64),
		URL: boundedUTF8(action.URL, 2048), PageState: boundedUTF8(action.PageState, 256),
		EvidenceCount: action.EvidenceCount, Failure: summarizeFailure(action.Failure),
	}
	for _, node := range action.TargetEvidence {
		summarized, omittedSelectors := summarizeNode(node)
		result.TargetEvidence = append(result.TargetEvidence, summarized)
		result.OmittedSelectors += omittedSelectors
	}
	originalEvidenceCount := max(action.EvidenceCount, len(action.TargetEvidence))
	result.TargetEvidence = deduplicateNodes(result.TargetEvidence)
	result.EvidenceCount = max(originalEvidenceCount, len(result.TargetEvidence))
	result.OmittedEvidence = originalEvidenceCount - len(result.TargetEvidence)
	return result
}

func summarizeNode(node rawNode) (ToolResultNodeSummary, int) {
	result := ToolResultNodeSummary{
		NodeID: boundedUTF8(node.NodeID, 128), BackendDOMNodeID: cloneRaw(node.BackendDOMNodeID),
		Role: boundedUTF8(node.Role, 128), Name: boundedUTF8(node.Name, 512),
		PageState: boundedUTF8(node.PageState, 256), Source: boundedUTF8(node.Source, 128),
		Focusable: node.Focusable, Disabled: node.Disabled,
	}
	if node.ParentID != nil {
		result.ParentID = boundedUTF8(*node.ParentID, 128)
	}
	if node.DOM.Tag != "" || len(node.DOM.Attrs) > 0 {
		result.DOM = &ToolResultDOMSummary{
			Tag:   boundedUTF8(node.DOM.Tag, 64),
			Attrs: make(map[string]string, len(node.DOM.Attrs)),
		}
		for key, value := range node.DOM.Attrs {
			result.DOM.Attrs[boundedUTF8(key, 128)] = boundedUTF8(value, 512)
		}
	}
	result.VerifiedSelectors = deduplicateSelectors(node.VerifiedSelectors)
	return result, len(node.VerifiedSelectors) - len(result.VerifiedSelectors)
}

func summarizeFailure(failure *rawFailure) *ToolResultErrorSummary {
	if failure == nil {
		return nil
	}
	return &ToolResultErrorSummary{
		Code: boundedUTF8(failure.Code, 128), Message: boundedUTF8(failure.Message, 2048),
		StepIndex: failure.StepIndex, ActionIndex: failure.ActionIndex,
		Action: boundedUTF8(failure.Action, 64), Target: boundedUTF8(failure.Target, 1024),
	}
}

func normalizeSummary(summary *ModelToolSummary) {
	summary.Failures = deduplicateFailures(summary.Failures)
	sort.Strings(summary.Warnings)
	pageByKey := make(map[string]ToolResultPageSummary)
	for _, page := range summary.Pages {
		page.A11yNodes = deduplicateNodes(page.A11yNodes)
		page.Actions = normalizeActions(page.Actions)
		key := page.URL + "\x00" + page.PageState
		existing, exists := pageByKey[key]
		if exists {
			pageByKey[key] = mergePageSummaries(existing, page)
		} else {
			pageByKey[key] = page
		}
	}
	summary.Pages = summary.Pages[:0]
	for _, page := range pageByKey {
		summary.Pages = append(summary.Pages, page)
		summary.Truncation.Omitted.Pages += page.Omitted.Pages
		summary.Truncation.Omitted.Actions += page.Omitted.Actions
		summary.Truncation.Omitted.Nodes += page.Omitted.Nodes
		summary.Truncation.Omitted.TargetEvidence += page.Omitted.TargetEvidence
		summary.Truncation.Omitted.Selectors += page.Omitted.Selectors
	}
	if summary.Truncation.Omitted != (ToolResultOmissionCounters{}) {
		summary.Truncation.Truncated = true
		summary.Truncation.Reason = "deterministic_filter"
	}
	sort.Slice(summary.Pages, func(i, j int) bool {
		left, right := summary.Pages[i], summary.Pages[j]
		if left.URL != right.URL {
			return left.URL < right.URL
		}
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		return left.Revision < right.Revision
	})
}

func mergePageSummaries(
	left ToolResultPageSummary,
	right ToolResultPageSummary,
) ToolResultPageSummary {
	merged := left
	if right.Revision > left.Revision ||
		(right.Revision == left.Revision && jsonLess(right, left)) {
		merged.URL = right.URL
		merged.PageState = right.PageState
		merged.PageKind = right.PageKind
		merged.Revision = right.Revision
		merged.Status = right.Status
		merged.Description = right.Description
		merged.ElementCount = right.ElementCount
		merged.Failure = right.Failure
	}
	if merged.PageKind == "" {
		merged.PageKind = firstNonEmptyString(left.PageKind, right.PageKind)
	}
	if merged.Status == "" {
		merged.Status = firstNonEmptyString(left.Status, right.Status)
	}
	merged.A11yNodes = deduplicateNodes(append(merged.A11yNodes, right.A11yNodes...))
	merged.Actions = normalizeActions(append(merged.Actions, right.Actions...))
	merged.Omitted = left.Omitted
	addOmissions(&merged.Omitted, right.Omitted)
	merged.Omitted.Pages++
	merged.ReferenceOnly = left.ReferenceOnly && right.ReferenceOnly
	return merged
}

func encodeBoundedSummary(summary *ModelToolSummary) (string, error) {
	for {
		encoded, err := encodeSummary(summary)
		if err != nil {
			return "", err
		}
		if len(encoded) <= ModelToolSummaryTargetBytes {
			return string(encoded), nil
		}
		if omitOneSummaryDetail(summary) {
			summary.Truncation.Truncated = true
			summary.Truncation.Reason = "per_summary_target"
			continue
		}
		makeSummaryReferenceOnly(summary, "per_summary_hard_limit")
		encoded, err = encodeSummary(summary)
		if err != nil {
			return "", err
		}
		for len(encoded) > ModelToolSummaryHardLimitBytes && len(summary.Pages) > 0 {
			summary.Truncation.Omitted.Pages++
			summary.Pages = summary.Pages[:len(summary.Pages)-1]
			encoded, err = encodeSummary(summary)
			if err != nil {
				return "", err
			}
		}
		if len(encoded) > ModelToolSummaryHardLimitBytes {
			return "", errors.New("reference-only tool summary exceeds hard limit")
		}
		return string(encoded), nil
	}
}

func encodeSummary(summary *ModelToolSummary) ([]byte, error) {
	summary.SummarySHA256 = ""
	if IsExplorationTool(summary.Tool) {
		summary.Observation = buildStructuredObservation(summary.Pages, summary.Failures)
	}
	for range 3 {
		encoded, err := json.Marshal(summary)
		if err != nil {
			return nil, err
		}
		summary.Truncation.SummaryBytes = len(encoded) + 84
	}
	unsigned, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	summary.SummarySHA256 = sha256HexBytes(unsigned)
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	summary.Truncation.SummaryBytes = len(encoded)
	unsigned, err = marshalUnsignedSummary(*summary)
	if err != nil {
		return nil, err
	}
	summary.SummarySHA256 = sha256HexBytes(unsigned)
	return json.Marshal(summary)
}

func marshalUnsignedSummary(summary ModelToolSummary) ([]byte, error) {
	summary.SummarySHA256 = ""
	return json.Marshal(summary)
}

func omitOneSummaryDetail(summary *ModelToolSummary) bool {
	pageIndex := pageWithMostNodes(summary.Pages)
	if pageIndex >= 0 {
		page := &summary.Pages[pageIndex]
		omitCount := max(1, len(page.A11yNodes)/4)
		page.A11yNodes = page.A11yNodes[:len(page.A11yNodes)-omitCount]
		page.Omitted.Nodes += omitCount
		summary.Truncation.Omitted.Nodes += omitCount
		return true
	}
	for pageIndex := len(summary.Pages) - 1; pageIndex >= 0; pageIndex-- {
		page := &summary.Pages[pageIndex]
		for actionIndex := len(page.Actions) - 1; actionIndex >= 0; actionIndex-- {
			action := &page.Actions[actionIndex]
			if len(action.TargetEvidence) == 0 {
				continue
			}
			omitCount := max(1, len(action.TargetEvidence)/4)
			action.TargetEvidence = action.TargetEvidence[:len(action.TargetEvidence)-omitCount]
			action.OmittedEvidence += omitCount
			page.Omitted.TargetEvidence += omitCount
			summary.Truncation.Omitted.TargetEvidence += omitCount
			return true
		}
	}
	for pageIndex := len(summary.Pages) - 1; pageIndex >= 0; pageIndex-- {
		page := &summary.Pages[pageIndex]
		if len(page.Actions) == 0 {
			continue
		}
		omitCount := max(1, len(page.Actions)/4)
		page.Actions = page.Actions[:len(page.Actions)-omitCount]
		page.Omitted.Actions += omitCount
		summary.Truncation.Omitted.Actions += omitCount
		return true
	}
	return false
}

func pageWithMostNodes(pages []ToolResultPageSummary) int {
	index := -1
	count := 0
	for candidate := range pages {
		if len(pages[candidate].A11yNodes) > count {
			index = candidate
			count = len(pages[candidate].A11yNodes)
		}
	}
	return index
}

func makePageReferenceOnly(page *ToolResultPageSummary) ToolResultOmissionCounters {
	omitted := ToolResultOmissionCounters{
		Actions: len(page.Actions),
		Nodes:   len(page.A11yNodes),
	}
	for _, node := range page.A11yNodes {
		omitted.Selectors += len(node.VerifiedSelectors)
	}
	for _, action := range page.Actions {
		omitted.TargetEvidence += len(action.TargetEvidence)
		for _, node := range action.TargetEvidence {
			omitted.Selectors += len(node.VerifiedSelectors)
		}
	}
	addOmissions(&page.Omitted, omitted)
	page.A11yNodes = nil
	page.Actions = nil
	page.ReferenceOnly = true
	if page.Failure == nil && page.Status == "" {
		page.Status = "referenced"
	}
	return omitted
}

func makeSummaryReferenceOnly(summary *ModelToolSummary, reason string) {
	for index := range summary.Pages {
		addOmissions(
			&summary.Truncation.Omitted,
			makePageReferenceOnly(&summary.Pages[index]),
		)
	}
	summary.ReferenceOnly = true
	summary.Truncation.Truncated = true
	summary.Truncation.Reason = reason
}

func addOmissions(
	target *ToolResultOmissionCounters,
	added ToolResultOmissionCounters,
) {
	target.Pages += added.Pages
	target.Actions += added.Actions
	target.Nodes += added.Nodes
	target.TargetEvidence += added.TargetEvidence
	target.Selectors += added.Selectors
}

func deduplicateNodes(nodes []ToolResultNodeSummary) []ToolResultNodeSummary {
	seen := make(map[string]ToolResultNodeSummary, len(nodes))
	for _, node := range nodes {
		key := node.NodeID
		if key == "" {
			key = node.PageState + "\x00" + node.Role + "\x00" + node.Name
		}
		existing, exists := seen[key]
		if !exists || len(node.VerifiedSelectors) > len(existing.VerifiedSelectors) ||
			(len(node.VerifiedSelectors) == len(existing.VerifiedSelectors) &&
				jsonLess(node, existing)) {
			seen[key] = node
		}
	}
	result := make([]ToolResultNodeSummary, 0, len(seen))
	for _, node := range seen {
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if nodePriority(left) != nodePriority(right) {
			return nodePriority(left) < nodePriority(right)
		}
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		if left.Role != right.Role {
			return left.Role < right.Role
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.NodeID < right.NodeID
	})
	return result
}

func deduplicateSelectors(selectors []ToolResultSelectorSummary) []ToolResultSelectorSummary {
	seen := make(map[string]ToolResultSelectorSummary, len(selectors))
	for _, selector := range selectors {
		selector.Strategy = boundedUTF8(selector.Strategy, 64)
		selector.Selector = boundedUTF8(selector.Selector, 1024)
		selector.Name = boundedUTF8(selector.Name, 512)
		selector.Source = boundedUTF8(selector.Source, 128)
		if selector.Selector == "" {
			continue
		}
		key := selector.Strategy + "\x00" + selector.Selector
		existing, exists := seen[key]
		if !exists || jsonLess(selector, existing) {
			seen[key] = selector
		}
	}
	result := make([]ToolResultSelectorSummary, 0, len(seen))
	for _, selector := range seen {
		result = append(result, selector)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Strategy != result[j].Strategy {
			return result[i].Strategy < result[j].Strategy
		}
		return result[i].Selector < result[j].Selector
	})
	return result
}

func deduplicateFailures(failures []ToolResultErrorSummary) []ToolResultErrorSummary {
	seen := make(map[string]ToolResultErrorSummary, len(failures))
	for _, failure := range failures {
		encoded, _ := json.Marshal(failure)
		seen[string(encoded)] = failure
	}
	result := make([]ToolResultErrorSummary, 0, len(seen))
	for _, failure := range seen {
		result = append(result, failure)
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := json.Marshal(result[i])
		right, _ := json.Marshal(result[j])
		return bytes.Compare(left, right) < 0
	})
	return result
}

func normalizeActions(actions []ToolResultActionSummary) []ToolResultActionSummary {
	seen := make(map[string]ToolResultActionSummary, len(actions))
	for index := range actions {
		actions[index].TargetEvidence = deduplicateNodes(actions[index].TargetEvidence)
		action := actions[index]
		key := fmt.Sprintf(
			"%d\x00%d\x00%s\x00%s\x00%s\x00%s",
			intValue(action.StepIndex),
			intValue(action.ActionIndex),
			action.Phase,
			action.Action,
			action.Target,
			action.Status,
		)
		existing, exists := seen[key]
		if !exists || jsonLess(action, existing) {
			seen[key] = action
		}
	}
	result := make([]ToolResultActionSummary, 0, len(seen))
	for _, action := range seen {
		result = append(result, action)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if intValue(left.StepIndex) != intValue(right.StepIndex) {
			return intValue(left.StepIndex) < intValue(right.StepIndex)
		}
		if intValue(left.ActionIndex) != intValue(right.ActionIndex) {
			return intValue(left.ActionIndex) < intValue(right.ActionIndex)
		}
		if left.Phase != right.Phase {
			return left.Phase < right.Phase
		}
		if left.Action != right.Action {
			return left.Action < right.Action
		}
		return left.Target < right.Target
	})
	return result
}

func nodePriority(node ToolResultNodeSummary) int {
	if len(node.VerifiedSelectors) > 0 {
		return 0
	}
	if node.Focusable || isInteractiveRole(node.Role) {
		return 1
	}
	if node.ParentID == "" {
		return 2
	}
	return 3
}

func isInteractiveRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "button", "checkbox", "combobox", "link", "menuitem", "option",
		"radio", "searchbox", "slider", "spinbutton", "switch", "tab",
		"textbox":
		return true
	default:
		return false
	}
}

func isSemanticRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "heading", "paragraph", "product", "status", "alert", "cell",
		"row", "listitem", "img":
		return true
	default:
		return false
	}
}

func buildStructuredObservation(
	pages []ToolResultPageSummary,
	failures []ToolResultErrorSummary,
) *StructuredObservation {
	observation := &StructuredObservation{SchemaVersion: StructuredObservationV1}
	groupByKey := make(map[string]*ObservedElementGroup)
	candidateByKey := make(map[string]ObservedCandidateCoverage)
	labelCounts := make(map[string]int)
	factByKey := make(map[string]ObservedVerificationFact)
	actionByKey := make(map[string]ObservedActionOption)
	for _, page := range pages {
		observation.PageStates = append(observation.PageStates, ObservedPageState{
			PageState:    page.PageState,
			PageKind:     firstNonEmptyString(page.PageKind, "unknown"),
			URL:          page.URL,
			Status:       page.Status,
			Revision:     page.Revision,
			ElementCount: page.ElementCount,
		})
		for _, node := range page.A11yNodes {
			category := classifyElement(node)
			groupKey := page.PageState + "\x00" + category
			group := groupByKey[groupKey]
			if group == nil {
				group = &ObservedElementGroup{
					PageState: page.PageState,
					Category:  category,
				}
				groupByKey[groupKey] = group
			}
			group.Count++
			if len(node.VerifiedSelectors) > 0 {
				group.VerifiedCount++
			}
			label := observationLabel(node)
			if label != "" && len(group.Representative) < 6 &&
				!containsString(group.Representative, label) {
				group.Representative = append(group.Representative, label)
			}
			if label != "" {
				labelCounts[node.PageState+"\x00"+category+"\x00"+label]++
				candidate := ObservedCandidateCoverage{
					PageState:     firstNonEmptyString(node.PageState, page.PageState),
					Category:      category,
					Label:         label,
					Role:          node.Role,
					SelectorCount: len(node.VerifiedSelectors),
					Executable:    len(node.VerifiedSelectors) > 0 && !node.Disabled,
				}
				if len(node.VerifiedSelectors) > 0 {
					candidate.PrimarySelector = node.VerifiedSelectors[0].Selector
					candidate.Source = firstNonEmptyString(
						node.VerifiedSelectors[0].Source,
						node.VerifiedSelectors[0].Strategy,
					)
				}
				candidateByKey[candidate.PageState+"\x00"+category+"\x00"+label] = candidate
			}
			if fact := verificationFactForNode(page.PageState, node); fact.Kind != "" {
				factByKey[fact.PageState+"\x00"+fact.Kind+"\x00"+fact.Label+"\x00"+fact.Selector] = fact
			}
		}
		for _, action := range page.Actions {
			option := ObservedActionOption{
				PageState:       firstNonEmptyString(action.PageState, page.PageState),
				URL:             action.URL,
				Action:          action.Action,
				Target:          action.Target,
				Status:          action.Status,
				EvidenceCount:   action.EvidenceCount,
				SideEffect:      actionSideEffect(action.Action, action.Target),
				IdempotencyHint: actionIdempotencyHint(action.Action, action.Target),
			}
			key := option.PageState + "\x00" + option.Action + "\x00" + option.Target + "\x00" + option.Status
			actionByKey[key] = option
		}
		if page.Failure != nil {
			observation.RecoveryHints = append(
				observation.RecoveryHints,
				recoveryHintFromFailure(*page.Failure),
			)
		}
	}
	for key, candidate := range candidateByKey {
		candidate.AmbiguousCount = labelCounts[key]
		candidateByKey[key] = candidate
	}
	for _, failure := range failures {
		observation.RecoveryHints = append(
			observation.RecoveryHints,
			recoveryHintFromFailure(failure),
		)
	}
	for _, group := range groupByKey {
		sort.Strings(group.Representative)
		observation.ElementGroups = append(observation.ElementGroups, *group)
	}
	for _, candidate := range candidateByKey {
		observation.CandidateCoverage = append(observation.CandidateCoverage, candidate)
	}
	for _, action := range actionByKey {
		observation.ActionOptions = append(observation.ActionOptions, action)
	}
	for _, fact := range factByKey {
		observation.VerificationFacts = append(observation.VerificationFacts, fact)
	}
	observation.RecoveryHints = deduplicateRecoveryHints(observation.RecoveryHints)
	normalizeStructuredObservation(observation)
	if len(observation.PageStates) == 0 &&
		len(observation.ElementGroups) == 0 &&
		len(observation.CandidateCoverage) == 0 &&
		len(observation.ActionOptions) == 0 &&
		len(observation.VerificationFacts) == 0 &&
		len(observation.RecoveryHints) == 0 {
		return nil
	}
	return observation
}

func classifyPageKind(url, pageState, description string) string {
	value := strings.ToLower(url + " " + pageState + " " + description)
	switch {
	case strings.Contains(value, "view_cart") || strings.Contains(value, "cart"):
		return "cart"
	case strings.Contains(value, "product_details"):
		return "product_detail"
	case strings.Contains(value, "search="):
		return "search_results"
	case strings.Contains(value, "/products"):
		return "products"
	case strings.Contains(value, "login"):
		return "login"
	case strings.Contains(value, "checkout"):
		return "checkout"
	default:
		return "unknown"
	}
}

func classifyElement(node ToolResultNodeSummary) string {
	role := strings.ToLower(strings.TrimSpace(node.Role))
	tag := ""
	inputType := ""
	if node.DOM != nil {
		tag = strings.ToLower(strings.TrimSpace(node.DOM.Tag))
		inputType = strings.ToLower(strings.TrimSpace(node.DOM.Attrs["type"]))
	}
	name := strings.ToLower(node.Name)
	switch {
	case role == "searchbox" || role == "textbox" || tag == "input" ||
		tag == "textarea" || inputType == "search":
		return "input"
	case role == "button" || tag == "button" || inputType == "submit":
		return "button"
	case role == "link" || tag == "a":
		return "link"
	case role == "spinbutton" || inputType == "number":
		return "numeric_input"
	case role == "product" || strings.Contains(name, "product"):
		return "product"
	case role == "row" || strings.Contains(name, "rs.") || strings.Contains(name, "blue top"):
		return "cart_or_product_row"
	case role == "heading":
		return "heading"
	case role == "cell":
		return "cell"
	default:
		return "text"
	}
}

func observationLabel(node ToolResultNodeSummary) string {
	if trimmed := strings.TrimSpace(node.Name); trimmed != "" {
		return boundedUTF8(trimmed, 256)
	}
	if node.DOM != nil {
		for _, key := range []string{"id", "name", "placeholder", "aria-label", "href"} {
			if value := strings.TrimSpace(node.DOM.Attrs[key]); value != "" {
				return boundedUTF8(value, 256)
			}
		}
	}
	return ""
}

func verificationFactForNode(
	pageState string,
	node ToolResultNodeSummary,
) ObservedVerificationFact {
	label := observationLabel(node)
	if label == "" {
		return ObservedVerificationFact{}
	}
	category := classifyElement(node)
	if category != "cart_or_product_row" && category != "cell" && category != "heading" {
		return ObservedVerificationFact{}
	}
	fact := ObservedVerificationFact{
		PageState: pageState,
		Kind:      category,
		Label:     label,
		Source:    firstNonEmptyString(node.Source, "a11y_or_dom"),
	}
	if len(node.VerifiedSelectors) > 0 {
		fact.Selector = node.VerifiedSelectors[0].Selector
	}
	return fact
}

func actionSideEffect(action, target string) string {
	value := strings.ToLower(action + " " + target)
	switch {
	case strings.Contains(value, "add to cart") ||
		strings.Contains(value, "submit") ||
		strings.Contains(value, "delete") ||
		strings.Contains(value, "checkout") ||
		strings.Contains(value, "place order"):
		return "external_or_business_state"
	case strings.Contains(value, "click") || strings.Contains(value, "input") ||
		strings.Contains(value, "fill") || strings.Contains(value, "goto"):
		return "browser_state"
	default:
		return "none"
	}
}

func actionIdempotencyHint(action, target string) string {
	value := strings.ToLower(action + " " + target)
	switch {
	case strings.Contains(value, "add to cart") ||
		strings.Contains(value, "submit") ||
		strings.Contains(value, "delete") ||
		strings.Contains(value, "place order"):
		return "non_idempotent"
	case strings.Contains(value, "input") || strings.Contains(value, "fill") ||
		strings.Contains(value, "goto") || strings.Contains(value, "wait") ||
		strings.Contains(value, "assert"):
		return "idempotent"
	default:
		return "unknown"
	}
}

func recoveryHintFromFailure(failure ToolResultErrorSummary) ObservedRecoveryHint {
	code := strings.ToLower(failure.Code + " " + failure.Message)
	switch {
	case strings.Contains(code, "locator") || strings.Contains(code, "match") ||
		strings.Contains(code, "selector") || strings.Contains(code, "not found"):
		return ObservedRecoveryHint{
			Category: "locator",
			Reason:   "Target evidence is missing, stale, ambiguous, or non-executable.",
			Action:   "re_explore_then_regenerate_dsl",
		}
	case strings.Contains(code, "condition") || strings.Contains(code, "precondition") ||
		strings.Contains(code, "postcondition"):
		return ObservedRecoveryHint{
			Category: "verification",
			Reason:   "A precondition or postcondition did not match the observed page state.",
			Action:   "inspect_condition_semantics_before_retry",
		}
	case strings.Contains(code, "navigation") || strings.Contains(code, "url"):
		return ObservedRecoveryHint{
			Category: "navigation",
			Reason:   "The browser did not reach the expected page state.",
			Action:   "re_explore_navigation_path",
		}
	default:
		return ObservedRecoveryHint{
			Category: "unknown",
			Reason:   "Failure could not be classified from structured fields.",
			Action:   "manual_reconcile",
		}
	}
}

func normalizeStructuredObservation(observation *StructuredObservation) {
	sort.Slice(observation.PageStates, func(i, j int) bool {
		left, right := observation.PageStates[i], observation.PageStates[j]
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		return left.URL < right.URL
	})
	sort.Slice(observation.ElementGroups, func(i, j int) bool {
		left, right := observation.ElementGroups[i], observation.ElementGroups[j]
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		return left.Category < right.Category
	})
	sort.Slice(observation.CandidateCoverage, func(i, j int) bool {
		left, right := observation.CandidateCoverage[i], observation.CandidateCoverage[j]
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		return left.Label < right.Label
	})
	sort.Slice(observation.ActionOptions, func(i, j int) bool {
		left, right := observation.ActionOptions[i], observation.ActionOptions[j]
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		if left.Action != right.Action {
			return left.Action < right.Action
		}
		return left.Target < right.Target
	})
	sort.Slice(observation.VerificationFacts, func(i, j int) bool {
		left, right := observation.VerificationFacts[i], observation.VerificationFacts[j]
		if left.PageState != right.PageState {
			return left.PageState < right.PageState
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Label < right.Label
	})
}

func deduplicateRecoveryHints(hints []ObservedRecoveryHint) []ObservedRecoveryHint {
	seen := make(map[string]ObservedRecoveryHint, len(hints))
	for _, hint := range hints {
		key := hint.Category + "\x00" + hint.Action
		seen[key] = hint
	}
	result := make([]ObservedRecoveryHint, 0, len(seen))
	for _, hint := range seen {
		result = append(result, hint)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Action < result[j].Action
	})
	return result
}

func deduplicateFailureBriefs(failures []ToolResultFailureBrief) []ToolResultFailureBrief {
	seen := make(map[string]ToolResultFailureBrief, len(failures))
	for _, failure := range failures {
		encoded, _ := json.Marshal(failure)
		seen[string(encoded)] = failure
	}
	result := make([]ToolResultFailureBrief, 0, len(seen))
	for _, failure := range seen {
		result = append(result, failure)
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := json.Marshal(result[i])
		right, _ := json.Marshal(result[j])
		return bytes.Compare(left, right) < 0
	})
	return result
}

func sortedKeys(values map[string]bool, limit int) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func boolValue(value any) (bool, bool) {
	typed, ok := value.(bool)
	return typed, ok
}

func arrayValue(value any) []any {
	typed, _ := value.([]any)
	return typed
}

func firstPresent(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if result, exists := value[key]; exists {
			return result
		}
	}
	return nil
}

func scalarValue(value any) any {
	switch typed := value.(type) {
	case nil, string, bool, float64, int, int64, json.Number:
		return typed
	default:
		return nil
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		result, _ := typed.Int64()
		return int(result)
	default:
		return 0
	}
}

func boundedUTF8(value string, limit int) string {
	if limit < 1 || len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func intValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sha256HexBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func jsonLess(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return bytes.Compare(leftBytes, rightBytes) < 0
}
