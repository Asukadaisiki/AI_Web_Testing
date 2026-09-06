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
	SchemaVersion string                   `json:"schema_version"`
	PolicyVersion string                   `json:"policy_version"`
	Tool          string                   `json:"tool"`
	Source        ToolResultSource         `json:"source"`
	SummarySHA256 string                   `json:"summary_sha256,omitempty"`
	Success       *bool                    `json:"success,omitempty"`
	Status        string                   `json:"status,omitempty"`
	Warnings      []string                 `json:"warnings,omitempty"`
	Failures      []ToolResultErrorSummary `json:"failures,omitempty"`
	Pages         []ToolResultPageSummary  `json:"pages,omitempty"`
	Truncation    ToolResultTruncation     `json:"truncation"`
	ReferenceOnly bool                     `json:"reference_only,omitempty"`
}

type rawExploreResult struct {
	URL          string       `json:"url"`
	PageState    string       `json:"page_state"`
	Revision     int          `json:"revision"`
	Status       string       `json:"status"`
	Warning      string       `json:"warning"`
	Success      *bool        `json:"success"`
	ElementCount int          `json:"element_count"`
	A11yNodes    []rawNode    `json:"a11y_nodes"`
	Actions      []rawAction  `json:"actions"`
	Failure      *rawFailure  `json:"failure"`
	Failures     []rawFailure `json:"failures"`
	Pages        []rawPage    `json:"pages"`
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
	if !IsExplorationTool(tool) {
		return string(content), nil
	}
	payload, err := NewToolResultEventPayload(tool, content)
	if err != nil {
		return "", err
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
	return encodeBoundedSummary(&summary)
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
		summary, ok := decodeModelToolSummary(message.Content)
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
		if _, ok := decodeModelToolSummary(message.Content); ok {
			total += len(message.Content)
		}
	}
	return total
}

func decodeModelToolSummary(content string) (ModelToolSummary, bool) {
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
		if !exists || page.Revision > existing.Revision ||
			(page.Revision == existing.Revision && jsonLess(page, existing)) {
			if exists {
				page.Omitted.Pages += 1 + existing.Omitted.Pages
			}
			pageByKey[key] = page
		} else {
			existing.Omitted.Pages += 1 + page.Omitted.Pages
			pageByKey[key] = existing
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
