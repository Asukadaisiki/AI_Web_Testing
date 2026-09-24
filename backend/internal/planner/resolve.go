package planner

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// 目标解析的错误码。
const (
	CodeTargetNotFound        = "target_not_found"
	CodeTargetAmbiguous       = "target_ambiguous"
	CodeTargetHintEmpty       = "target_hint_empty"
	CodeScopeNotFound         = "scope_not_found"
	CodeCandidateNotFound     = "candidate_not_found"
	CodeCandidateIncompatible = "candidate_incompatible"
)

// Result 是回给模型的工具结果。模型据此决定下一步或改口。
type Result struct {
	OK         bool            `json:"ok"`
	Summary    string          `json:"summary,omitempty"`
	Warning    string          `json:"warning,omitempty"`
	Error      string          `json:"error,omitempty"`
	Detail     string          `json:"detail,omitempty"`
	Page       *PageView       `json:"page,omitempty"`
	Candidates []CandidateView `json:"candidates,omitempty"`
	Steps      []StepView      `json:"steps,omitempty"`
	Failure    *DryRunFailure  `json:"dry_run_failure,omitempty"`
}

// CompactResult 保留规划下一轮需要的结果，不重复携带页面与完整步骤。
type CompactResult struct {
	OK      bool           `json:"ok"`
	Summary string         `json:"summary,omitempty"`
	Warning string         `json:"warning,omitempty"`
	Error   string         `json:"error,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Failure *DryRunFailure `json:"dry_run_failure,omitempty"`
}

// FailureSignature 描述在某个页面状态上失败过的动作策略。
//
// Task 5 会负责生成、去重并限制这份 ledger；Task 2 只负责把它放进快照。
type FailureSignature struct {
	PageFingerprint string          `json:"page_fingerprint"`
	Action          contract.Action `json:"action"`
	TargetKey       string          `json:"target_key"`
	ErrorCode       string          `json:"error_code"`
}

// StateSnapshot 是每次模型调用看到的唯一规划状态。
type StateSnapshot struct {
	Version    int                `json:"version"`
	Steps      []StepView         `json:"committed_steps"`
	Page       *PageView          `json:"current_page,omitempty"`
	LastResult *CompactResult     `json:"last_result,omitempty"`
	Failures   []FailureSignature `json:"failure_ledger,omitempty"`
}

// PageView 是给模型看的紧凑观测视图。
type PageView struct {
	URL              string                `json:"url"`
	Title            string                `json:"title"`
	Truncated        bool                  `json:"truncated,omitempty"`
	TruncationReason string                `json:"truncation_reason,omitempty"`
	Blockers         []BlockerView         `json:"blockers,omitempty"`
	Scopes           []ScopeView           `json:"scopes,omitempty"`
	ActionCandidates []ActionCandidateView `json:"action_candidates,omitempty"`
	Elements         []ElementView         `json:"elements"`
}

// ElementView 是页面上的一个可选目标。
type ElementView struct {
	Ref          string `json:"ref"`
	Tag          string `json:"tag"`
	Role         string `json:"role,omitempty"`
	Name         string `json:"name,omitempty"`
	Text         string `json:"text,omitempty"`
	Value        string `json:"value,omitempty"`
	ContainerRef string `json:"container_ref,omitempty"`
	LocatorKind  string `json:"locator_kind,omitempty"`
}

type ScopeView struct {
	Ref       string `json:"ref"`
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	ParentRef string `json:"parent_ref,omitempty"`
}

type BlockerView struct {
	Kind              string `json:"kind"`
	Ref               string `json:"ref,omitempty"`
	Confidence        string `json:"confidence,omitempty"`
	CoversTargetRef   string `json:"covers_target_ref,omitempty"`
	DismissCandidates int    `json:"dismiss_candidates,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type ActionCandidateView struct {
	CandidateID string                       `json:"candidate_id"`
	Kind        string                       `json:"kind"`
	Action      string                       `json:"action"`
	TargetRef   string                       `json:"target_ref"`
	Role        string                       `json:"role,omitempty"`
	Name        string                       `json:"name,omitempty"`
	Text        string                       `json:"text,omitempty"`
	Aliases     []string                     `json:"aliases,omitempty"`
	Attributes  map[string]string            `json:"attributes,omitempty"`
	Relations   []contract.CandidateRelation `json:"relations,omitempty"`
	Confidence  string                       `json:"confidence,omitempty"`
}

// CandidateView 是解析失败时给出的候选，带匹配分。
type CandidateView struct {
	Ref   string `json:"ref"`
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text,omitempty"`
	Score int    `json:"score"`
}

// StepView 是已记录步骤的摘要，供模型复核。
type StepView struct {
	Index          int      `json:"index"`
	Action         string   `json:"action"`
	Intent         string   `json:"intent"`
	Value          string   `json:"value,omitempty"`
	Target         string   `json:"target,omitempty"`
	Preconditions  []string `json:"preconditions"`
	Postconditions []string `json:"postconditions"`
}

// DryRunStep 是干跑失败的单个步骤。
type DryRunStep struct {
	Index       int                        `json:"index"`
	Action      string                     `json:"action"`
	Status      string                     `json:"status"`
	URL         string                     `json:"url,omitempty"`
	Error       string                     `json:"error,omitempty"`
	Unsatisfied []string                   `json:"unsatisfied_conditions,omitempty"`
	Blocker     *contract.Blocker          `json:"blocker,omitempty"`
	HitTest     *contract.HitTest          `json:"hit_test,omitempty"`
	Recovery    []contract.RecoveryAttempt `json:"recovery,omitempty"`
}

// DryRunFailure 是干跑失败的汇总，直接回给模型让它改。
type DryRunFailure struct {
	ExecutionID string       `json:"execution_id"`
	Status      string       `json:"status"`
	FinalURL    string       `json:"final_url,omitempty"`
	CurrentURL  string       `json:"current_url,omitempty"`
	Error       string       `json:"error,omitempty"`
	FailedStep  *DryRunStep  `json:"failed_step,omitempty"`
	Steps       []DryRunStep `json:"steps"`
	RepairHints []string     `json:"repair_hints,omitempty"`
}

type targetError struct {
	code    string
	message string
}

func (e *targetError) Error() string { return e.message }

func targetFailure(code, format string, args ...any) *targetError {
	return &targetError{code: code, message: fmt.Sprintf(format, args...)}
}

type scoredElement struct {
	element contract.Element
	score   int
}

// resolve 把模型的 hint 解析成唯一元素与已验证定位器。
//
// 只在"可见且可用"的元素里匹配；命中不唯一时一律拒绝，要求模型说得更具体。
// 这一步是"AI 生成错的目标"的拦截点。
func resolve(
	hint string, elements []contract.Element,
) (contract.Element, contract.Locator, []CandidateView, error) {
	if strings.TrimSpace(hint) == "" {
		return contract.Element{}, contract.Locator{}, nil,
			targetFailure(CodeTargetHintEmpty, "target hint must not be empty")
	}
	actionable := make([]scoredElement, 0, len(elements))
	for _, element := range elements {
		if !element.Visible || !element.Enabled || len(element.Locators) == 0 {
			continue
		}
		actionable = append(actionable, scoredElement{element: element, score: matchScore(hint, element)})
	}
	sort.SliceStable(actionable, func(i, j int) bool {
		if actionable[i].score != actionable[j].score {
			return actionable[i].score > actionable[j].score
		}
		return actionable[i].element.Ref < actionable[j].element.Ref
	})

	describe := func(limit int, includeZero bool) []CandidateView {
		views := make([]CandidateView, 0, limit)
		for _, item := range actionable {
			if len(views) >= limit {
				break
			}
			if item.score == 0 && !includeZero {
				continue
			}
			views = append(views, CandidateView{
				Ref:   item.element.Ref,
				Role:  item.element.Role,
				Name:  item.element.Name,
				Text:  item.element.Text,
				Score: item.score,
			})
		}
		return views
	}

	if len(actionable) == 0 {
		return contract.Element{}, contract.Locator{}, nil, targetFailure(
			CodeTargetNotFound,
			"the current page has no visible, enabled element to target; call open_page to move to another page",
		)
	}
	best := actionable[0]
	if best.score == 0 {
		return contract.Element{}, contract.Locator{}, describe(10, true), targetFailure(
			CodeTargetNotFound,
			"no element matches hint %q; pick one of the candidates or use a hint that appears verbatim in name/text",
			hint,
		)
	}
	if len(actionable) > 1 && actionable[1].score == best.score {
		return contract.Element{}, contract.Locator{}, describe(10, false), targetFailure(
			CodeTargetAmbiguous,
			"hint %q matches %d elements equally well; use a more specific hint",
			hint, countTopScore(actionable, best.score),
		)
	}
	return best.element, best.element.Locators[0], nil, nil
}

func resolveSpec(
	spec contract.TargetSpec, observation contract.Observation,
) (contract.Element, contract.Locator, []CandidateView, error) {
	object := normalizeTargetObject(spec)
	elements := observation.Elements
	if spec.Scope != nil {
		scoped, err := elementsInScope(*spec.Scope, observation)
		if err != nil {
			return contract.Element{}, contract.Locator{}, scopeCandidates(observation, 10), err
		}
		elements = scoped
	}
	hints := objectHints(object)
	if len(hints) == 0 && object.Role == "" {
		return contract.Element{}, contract.Locator{}, nil,
			targetFailure(CodeTargetHintEmpty, "target object must include text, name, or aliases")
	}

	actionable := make([]scoredElement, 0, len(elements))
	for _, element := range elements {
		if !element.Visible || !element.Enabled || len(element.Locators) == 0 {
			continue
		}
		if object.Role != "" && element.Role != object.Role {
			continue
		}
		actionable = append(actionable, scoredElement{
			element: element,
			score:   matchScoreSpec(hints, element),
		})
	}
	sort.SliceStable(actionable, func(i, j int) bool {
		if actionable[i].score != actionable[j].score {
			return actionable[i].score > actionable[j].score
		}
		return actionable[i].element.Ref < actionable[j].element.Ref
	})
	describe := func(limit int, includeZero bool) []CandidateView {
		views := make([]CandidateView, 0, limit)
		for _, item := range actionable {
			if len(views) >= limit {
				break
			}
			if item.score == 0 && !includeZero {
				continue
			}
			views = append(views, CandidateView{
				Ref:   item.element.Ref,
				Role:  item.element.Role,
				Name:  item.element.Name,
				Text:  item.element.Text,
				Score: item.score,
			})
		}
		return views
	}
	if len(actionable) == 0 {
		return contract.Element{}, contract.Locator{}, nil, targetFailure(
			CodeTargetNotFound,
			"the resolved scope has no visible, enabled element to target",
		)
	}
	best := actionable[0]
	if best.score == 0 {
		return contract.Element{}, contract.Locator{}, describe(10, true), targetFailure(
			CodeTargetNotFound,
			"no element matches target object; use text, name, or aliases from the current observation",
		)
	}
	if len(actionable) > 1 && actionable[1].score == best.score {
		return contract.Element{}, contract.Locator{}, describe(10, false), targetFailure(
			CodeTargetAmbiguous,
			"target object matches %d elements equally well; add or narrow a scope",
			countTopScore(actionable, best.score),
		)
	}
	return best.element, best.element.Locators[0], nil, nil
}

func resolveActionCandidate(
	action contract.Action, candidateID string, observation contract.Observation,
) (contract.Element, contract.Locator, contract.ActionCandidate, error) {
	if strings.TrimSpace(candidateID) == "" {
		return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
			targetFailure(CodeCandidateNotFound, "candidate_id must not be empty")
	}
	for _, candidate := range observation.ActionCandidates {
		if candidate.CandidateID != candidateID {
			continue
		}
		if candidate.Action != string(action) {
			return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
				targetFailure(
					CodeCandidateIncompatible,
					"candidate %q supports action %q, not %q",
					candidateID, candidate.Action, action,
				)
		}
		for _, element := range observation.Elements {
			if element.Ref != candidate.TargetRef {
				continue
			}
			if !element.Visible || !element.Enabled {
				return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
					targetFailure(
						CodeTargetNotFound,
						"candidate %q target %q is not visible and enabled in the latest observation",
						candidateID, candidate.TargetRef,
					)
			}
			for _, locator := range element.Locators {
				if locator == candidate.Locator {
					return element, candidate.Locator, candidate, nil
				}
			}
			return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
				targetFailure(
					CodeTargetNotFound,
					"candidate %q locator is not verified on target %q in the latest observation",
					candidateID, candidate.TargetRef,
				)
		}
		return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
			targetFailure(
				CodeTargetNotFound,
				"candidate %q target %q is absent from the latest observation",
				candidateID, candidate.TargetRef,
			)
	}
	return contract.Element{}, contract.Locator{}, contract.ActionCandidate{},
		targetFailure(
			CodeCandidateNotFound,
			"candidate %q is not present in the latest observation; call open_page again before using stale candidates",
			candidateID,
		)
}

func normalizeTargetObject(spec contract.TargetSpec) contract.TargetObject {
	object := spec.Object
	if object.Role == "" {
		object.Role = spec.Role
	}
	if object.Text == "" {
		object.Text = spec.Text
	}
	if object.Name == "" {
		object.Name = spec.Name
	}
	if len(object.Aliases) == 0 {
		object.Aliases = spec.Aliases
	}
	return object
}

func elementsInScope(scope contract.TargetScope, observation contract.Observation) ([]contract.Element, error) {
	scopeRefs := map[string]bool{}
	for _, node := range observation.Structures {
		if scope.Ref != "" && node.Ref != scope.Ref {
			continue
		}
		if scope.Kind != "" && node.Kind != scope.Kind {
			continue
		}
		if scope.ContainsText != "" && !scopeHasText(node, observation.Elements, scope.ContainsText) {
			continue
		}
		scopeRefs[node.Ref] = true
	}
	if len(scopeRefs) == 0 {
		return nil, targetFailure(
			CodeScopeNotFound,
			"no %s scope contains %q",
			scope.Kind,
			scope.ContainsText,
		)
	}
	addDescendantScopes(scopeRefs, observation.Structures)
	out := make([]contract.Element, 0, len(observation.Elements))
	for _, element := range observation.Elements {
		if scopeRefs[element.ContainerRef] || scopeRefs[element.ParentRef] {
			out = append(out, element)
		}
	}
	return out, nil
}

func addDescendantScopes(scopeRefs map[string]bool, nodes []contract.StructureNode) {
	changed := true
	for changed {
		changed = false
		for _, node := range nodes {
			if node.ParentRef == "" || !scopeRefs[node.ParentRef] || scopeRefs[node.Ref] {
				continue
			}
			scopeRefs[node.Ref] = true
			changed = true
		}
	}
}

func scopeHasText(node contract.StructureNode, elements []contract.Element, text string) bool {
	needle := normalize(text)
	if needle == "" {
		return true
	}
	if strings.Contains(normalize(node.FullText), needle) {
		return true
	}
	for _, element := range elements {
		if element.ContainerRef != node.Ref && element.ParentRef != node.Ref {
			continue
		}
		if strings.Contains(normalize(element.Name), needle) ||
			strings.Contains(normalize(element.Text), needle) ||
			strings.Contains(normalize(element.FullText), needle) ||
			strings.Contains(normalizeAlias(strings.Join(attributeValues(element.Attributes), " ")), normalizeAlias(text)) {
			return true
		}
	}
	return false
}

func attributeValues(attributes map[string]string) []string {
	values := make([]string, 0, len(attributes)*2)
	for key, value := range attributes {
		values = append(values, key, value)
	}
	return values
}

func objectHints(object contract.TargetObject) []string {
	raw := []string{object.Name, object.Text}
	raw = append(raw, object.Aliases...)
	seen := map[string]bool{}
	hints := make([]string, 0, len(raw))
	for _, value := range raw {
		normalized := normalize(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		hints = append(hints, value)
	}
	return hints
}

func matchScoreSpec(hints []string, element contract.Element) int {
	best := 0
	if len(hints) == 0 {
		return 50
	}
	for _, hint := range hints {
		score := matchScore(hint, element)
		if score > best {
			best = score
		}
		if attrScore := attributeScore(hint, element.Attributes); attrScore > best {
			best = attrScore
		}
	}
	return best
}

func attributeScore(hint string, attributes map[string]string) int {
	needle := normalizeAlias(hint)
	if needle == "" {
		return 0
	}
	best := 0
	for key, value := range attributes {
		haystack := normalizeAlias(key + " " + value)
		switch {
		case haystack == needle:
			if best < 90 {
				best = 90
			}
		case strings.Contains(haystack, needle) || strings.Contains(needle, haystack):
			if best < 75 {
				best = 75
			}
		case tokenOverlap(needle, haystack) >= 2:
			if best < 65 {
				best = 65
			}
		}
	}
	return best
}

func normalizeAlias(value string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ", ".", " ")
	return normalize(replacer.Replace(value))
}

func tokenOverlap(a, b string) int {
	tokens := map[string]bool{}
	for _, token := range strings.Fields(a) {
		tokens[token] = true
	}
	count := 0
	for _, token := range strings.Fields(b) {
		if tokens[token] {
			count++
		}
	}
	return count
}

func scopeCandidates(observation contract.Observation, limit int) []CandidateView {
	views := make([]CandidateView, 0, limit)
	for _, node := range observation.Structures {
		if len(views) >= limit {
			break
		}
		views = append(views, CandidateView{
			Ref:   node.Ref,
			Role:  node.Kind,
			Text:  node.FullText,
			Score: 0,
		})
	}
	return views
}

func countTopScore(items []scoredElement, score int) int {
	count := 0
	for _, item := range items {
		if item.score == score {
			count++
		}
	}
	return count
}

// matchScore 给元素与 hint 的匹配程度打分。0 表示不匹配。
func matchScore(hint string, element contract.Element) int {
	needle := normalize(hint)
	name := normalize(element.Name)
	text := normalize(element.Text)
	switch {
	case name != "" && name == needle:
		return 100
	case text != "" && text == needle:
		return 95
	case name != "" && strings.Contains(name, needle):
		return 80
	case text != "" && strings.Contains(text, needle):
		return 70
	case name != "" && strings.Contains(needle, name):
		return 60
	case text != "" && strings.Contains(needle, text):
		return 50
	}
	return 0
}

// normalize 统一大小写与空白，避免因排版差异错过匹配。
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func origin(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func isAbsoluteURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != ""
}
