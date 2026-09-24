// Package planner 把模型的工具调用翻译成 case 工件。
//
// 这是"模型不写 JSON"的落点：
//   - 每条工具调用当场校验（contract 的派生函数内部就是校验器）；
//   - click / input 的目标必须当场对最近一次真实观测解析成功，否则拒绝；
//   - finish_case 会在全新浏览器上下文里干跑一遍，跑通才允许进入审批。
//
// 因此旧版的两类失败（生成的 DSL 过不了校验、生成的就是错的）在结构上被消灭。
package planner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

// Planner 是一次规划过程中的 case 构建器。
type Planner struct {
	client *worker.Client
	// sessionID 是领域会话（CONTRACT §9）：观测截图按它落目录。
	sessionID string
	// browserSessionID 是执行器里的浏览器上下文句柄，用完即弃，与 sessionID 无关。
	browserSessionID string
	goal             string
	baseURL          string

	observation contract.Observation
	hasPage     bool
	steps       []contract.Step
	failures    []FailureSignature
}

// New 开一个作者态浏览器会话，并把它绑到领域会话上（决定产物落哪个目录）。
func New(ctx context.Context, client *worker.Client, sessionID, goal string) (*Planner, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("planning requires a session id")
	}
	browserSessionID, err := client.OpenSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("open authoring session: %w", err)
	}
	return &Planner{
		client:           client,
		sessionID:        sessionID,
		browserSessionID: browserSessionID,
		goal:             goal,
	}, nil
}

// Close 关闭作者态浏览器会话。
func (p *Planner) Close(ctx context.Context) {
	if p.browserSessionID == "" {
		return
	}
	_ = p.client.CloseSession(ctx, p.browserSessionID)
	p.browserSessionID = ""
}

// Steps 返回当前已构建的步骤。
func (p *Planner) Steps() []contract.Step {
	out := make([]contract.Step, len(p.steps))
	copy(out, p.steps)
	return out
}

// Snapshot 返回下一次模型调用所需的完整、紧凑规划状态。
func (p *Planner) Snapshot(lastResult *Result) StateSnapshot {
	compact := compactResult(lastResult, p.steps)
	snapshot := StateSnapshot{
		Version:    1,
		Steps:      stepViews(p.steps),
		LastResult: compact,
		Failures:   append([]FailureSignature{}, p.failures...),
	}
	if p.hasPage {
		snapshot.Page = BuildPageView(p.observation, p.goal, compact)
	}
	return snapshot
}

func compactResult(result *Result, steps []contract.Step) *CompactResult {
	if result == nil {
		return nil
	}
	compact := &CompactResult{
		OK:      result.OK,
		Summary: result.Summary,
		Warning: result.Warning,
		Error:   result.Error,
		Detail:  result.Detail,
		Failure: result.Failure,
	}
	if result.OK && len(steps) > 0 {
		compact.RelevanceTerms = stepRelevanceTerms(steps[len(steps)-1])
	}
	return compact
}

// Build 组装 case（不落库）。
func (p *Planner) Build(name string) (contract.Case, error) {
	if len(p.steps) == 0 {
		return contract.Case{}, fmt.Errorf("no steps have been authored yet")
	}
	artifact := contract.Case{
		CaseVersion: contract.CaseVersion,
		Name:        name,
		Goal:        p.goal,
		BaseURL:     p.baseURL,
		Steps:       p.Steps(),
	}
	if err := artifact.Validate(); err != nil {
		return contract.Case{}, err
	}
	return artifact, nil
}

// DryRun 在全新上下文里跑一遍，作为"生成的就是对的"的证明。
//
// 干跑的截图同样落进本次会话的产物目录——它也是这个会话的证据。
func (p *Planner) DryRun(ctx context.Context, artifact contract.Case) (contract.ExecutionResult, error) {
	return p.client.Execute(ctx, p.sessionID, artifact)
}

func (p *Planner) openPage(ctx context.Context, url, intent string) (Result, error) {
	if !isAbsoluteURL(url) {
		return failure("url_not_absolute", fmt.Sprintf("open_page requires an absolute url, got %q", url)), nil
	}
	observation, err := p.client.Navigate(ctx, p.browserSessionID, url)
	if err != nil {
		return workerFailure("navigate_failed", err), nil
	}
	step, err := contract.DeriveGotoStep(len(p.steps), intent, url)
	if err != nil {
		return failure("step_rejected", err.Error()), nil
	}
	if len(p.steps) == 0 {
		p.baseURL = origin(url)
	}
	p.steps = append(p.steps, step)
	p.observation = observation
	p.hasPage = true
	return Result{
		OK:      true,
		Summary: fmt.Sprintf("navigated to %s and recorded goto step %d", observation.URL, step.Index),
		Page:    pageView(observation),
		Steps:   stepViews(p.steps),
	}, nil
}

func (p *Planner) click(
	ctx context.Context, hint string, spec *contract.TargetSpec, candidateID string, intent string, expects contract.Expects,
) (Result, error) {
	return p.act(ctx, contract.ActionClick, hint, spec, candidateID, nil, intent, expects, false)
}

func (p *Planner) input(
	ctx context.Context, hint string, spec *contract.TargetSpec, candidateID string, value, intent string, expects contract.Expects, submit bool,
) (Result, error) {
	return p.act(ctx, contract.ActionInput, hint, spec, candidateID, &value, intent, expects, submit)
}

func (p *Planner) targetAction(
	ctx context.Context,
	action contract.Action,
	hint string,
	spec *contract.TargetSpec,
	candidateID string,
	value string,
	intent string,
	expects contract.Expects,
) (Result, error) {
	var ptr *string
	if action == contract.ActionSelect || action == contract.ActionUploadFile {
		ptr = &value
	}
	return p.act(ctx, action, hint, spec, candidateID, ptr, intent, expects, false)
}

func (p *Planner) act(
	ctx context.Context,
	action contract.Action,
	hint string,
	spec *contract.TargetSpec,
	requestedCandidateID string,
	value *string,
	intent string,
	expects contract.Expects,
	submit bool,
) (Result, error) {
	if !p.hasPage {
		return failure("no_observation", "no page has been observed yet; call open_page first"), nil
	}
	targetKey := failureTargetKey(requestedCandidateID, spec, hint)
	element, locator, selectedCandidateID, candidates, err := p.resolveTarget(action, hint, spec, requestedCandidateID)
	if err != nil {
		return Result{
			OK:         false,
			Error:      errorCode(err),
			Detail:     err.Error(),
			Page:       pageView(p.observation),
			Candidates: candidates,
		}, nil
	}
	targetHint := hint
	if strings.TrimSpace(targetHint) == "" {
		targetHint = selectedCandidateID
		if strings.TrimSpace(intent) != "" {
			targetHint = intent
		}
	}
	target := contract.Target{
		Hint:    targetHint,
		Locator: locator,
		Grounding: contract.Grounding{
			ObservationID: p.observation.ObservationID,
			PageStateID:   p.observation.PageStateID,
			CandidateID:   selectedCandidateID,
			PageURL:       p.observation.URL,
		},
		Spec: spec,
	}
	step, err := contract.DeriveActionStep(
		len(p.steps), action, intent, target, value, submit, p.observation.URL, expects,
	)
	if err != nil {
		return failure("step_rejected", err.Error()), nil
	}
	pageFingerprint := PageFingerprint(p.observation)
	if p.failedStrategy(pageFingerprint, action, targetKey) {
		return failure(
			CodeStrategyRepeated,
			"this action and target already failed on the unchanged page; change target, scope, action, or page state",
		), nil
	}
	if isTargetAssertion(action) {
		p.steps = append(p.steps, step)
		return Result{
			OK:      true,
			Summary: fmt.Sprintf("recorded %s step %d on %q", action, step.Index, displayName(element)),
			Page:    pageView(p.observation),
			Steps:   stepViews(p.steps),
		}, nil
	}
	// 先真的执行动作，成功后才记录步骤：动作失败不该留下一条假步骤。
	request := contract.ActRequest{
		Action:         action,
		Locator:        locator,
		Submit:         submit,
		Postconditions: step.Postconditions,
	}
	if value != nil {
		request.Value = *value
	}
	response, err := p.client.Act(ctx, p.browserSessionID, request)
	if err != nil {
		result := workerFailure("action_failed", err)
		p.rememberFailure(FailureSignature{
			PageFingerprint: pageFingerprint,
			Action:          action,
			TargetKey:       targetKey,
			ErrorCode:       result.Error,
		})
		return p.restoreAfterFailedAction(ctx, result)
	}
	if response.Status != "passed" {
		code := "action_failed"
		detail := fmt.Sprintf("worker returned authoring action status %q", response.Status)
		if response.Error != nil {
			code = string(response.Error.Kind)
			detail = response.Error.Message
		}
		result := Result{
			OK:     false,
			Error:  code,
			Detail: detail,
		}
		p.rememberFailure(FailureSignature{
			PageFingerprint: pageFingerprint,
			Action:          action,
			TargetKey:       targetKey,
			ErrorCode:       result.Error,
		})
		return p.restoreAfterFailedAction(ctx, result)
	}
	p.observation = response.Observation
	p.steps = append(p.steps, step)
	result := Result{
		OK:      true,
		Summary: fmt.Sprintf("recorded %s step %d on %q", action, step.Index, displayName(element)),
		Page:    pageView(response.Observation),
		Steps:   stepViews(p.steps),
	}
	return result, nil
}

func (p *Planner) restoreAfterFailedAction(ctx context.Context, result Result) (Result, error) {
	if err := p.ReplayCommitted(ctx, len(p.steps)); err != nil {
		return Result{}, &FatalError{Code: CodeCommittedPrefixReplayFailed, Err: err}
	}
	result.Steps = stepViews(p.steps)
	if p.hasPage {
		result.Page = pageView(p.observation)
	}
	return result, nil
}

func isTargetAssertion(action contract.Action) bool {
	return action == contract.ActionAssertElement ||
		action == contract.ActionAssertAttribute ||
		action == contract.ActionAssertCount
}

func displayName(element contract.Element) string {
	if strings.TrimSpace(element.Name) != "" {
		return element.Name
	}
	if strings.TrimSpace(element.Text) != "" {
		return element.Text
	}
	return element.Ref
}

func (p *Planner) resolveTarget(
	action contract.Action, hint string, spec *contract.TargetSpec, requestedCandidateID string,
) (contract.Element, contract.Locator, string, []CandidateView, error) {
	if strings.TrimSpace(requestedCandidateID) != "" {
		element, locator, candidate, err := resolveActionCandidate(action, requestedCandidateID, p.observation)
		if err != nil {
			return contract.Element{}, contract.Locator{}, "", nil, err
		}
		return element, locator, candidate.CandidateID, nil, nil
	}
	if spec == nil {
		element, locator, candidates, err := resolve(hint, p.observation.Elements)
		if err != nil {
			return contract.Element{}, contract.Locator{}, "", candidates, err
		}
		return element, locator, candidateID(element, locator), nil, nil
	}
	if len(objectHints(spec.Object)) == 0 && strings.TrimSpace(hint) != "" {
		specCopy := *spec
		specCopy.Object.Text = hint
		spec = &specCopy
	}
	element, locator, candidates, err := resolveSpec(*spec, p.observation)
	if err != nil {
		return contract.Element{}, contract.Locator{}, "", candidates, err
	}
	return element, locator, candidateID(element, locator), nil, nil
}

func (p *Planner) assertText(text, intent string) (Result, error) {
	if !p.hasPage {
		return failure("no_observation", "no page has been observed yet; call open_page first"), nil
	}
	step, err := contract.DeriveAssertTextStep(len(p.steps), intent, text, p.observation.URL)
	if err != nil {
		return failure("step_rejected", err.Error()), nil
	}
	p.steps = append(p.steps, step)
	result := Result{
		OK:      true,
		Summary: fmt.Sprintf("recorded assert_text step %d", step.Index),
		Page:    pageView(p.observation),
		Steps:   stepViews(p.steps),
	}
	if !observationHasText(p.observation, text) {
		result.Warning = fmt.Sprintf(
			"the text %q is not present in the current observation; the step was recorded but the dry run will fail unless the page changes first",
			text,
		)
	}
	return result, nil
}

func (p *Planner) assertURL(contains, intent string) (Result, error) {
	if !p.hasPage {
		return failure("no_observation", "no page has been observed yet; call open_page first"), nil
	}
	step, err := contract.DeriveAssertURLStep(len(p.steps), intent, contains, p.observation.URL)
	if err != nil {
		return failure("step_rejected", err.Error()), nil
	}
	p.steps = append(p.steps, step)
	result := Result{
		OK:      true,
		Summary: fmt.Sprintf("recorded assert_url step %d", step.Index),
		Page:    pageView(p.observation),
		Steps:   stepViews(p.steps),
	}
	if !strings.Contains(p.observation.URL, contains) {
		result.Warning = fmt.Sprintf(
			"the current url %q does not contain %q; the step was recorded but the dry run will fail unless the page changes first",
			p.observation.URL, contains,
		)
	}
	return result, nil
}

// Finish 校验 + 干跑，跑通才返回可落库的工件。
func (p *Planner) Finish(ctx context.Context, name string) (contract.Case, contract.ExecutionResult, Result, error) {
	artifact, err := p.Build(name)
	if err != nil {
		code := contract.CodeOf(err)
		if code == "" {
			code = "case_invalid"
		}
		return contract.Case{}, contract.ExecutionResult{}, failure(code, err.Error()), nil
	}
	result, err := p.DryRun(ctx, artifact)
	if err != nil {
		return contract.Case{}, contract.ExecutionResult{}, workerFailure("dry_run_failed", err), nil
	}
	if result.Status != contract.ExecutionPassed {
		repair, err := p.PrepareDryRunRepair(ctx, result)
		return contract.Case{}, result, repair, err
	}
	return artifact, result, Result{
		OK:      true,
		Summary: fmt.Sprintf("the case passed a full dry run (%d steps); it is ready for approval", len(artifact.Steps)),
		Steps:   stepViews(p.steps),
	}, nil
}

func errorCode(err error) string {
	if violation, ok := err.(*targetError); ok {
		return violation.code
	}
	return "target_error"
}

func candidateID(element contract.Element, locator contract.Locator) string {
	for index, candidate := range element.Locators {
		if candidate == locator {
			return fmt.Sprintf("%s:%d", element.Ref, index)
		}
	}
	return element.Ref + ":0"
}

func stepViews(steps []contract.Step) []StepView {
	views := make([]StepView, 0, len(steps))
	for _, step := range steps {
		view := StepView{
			Index:          step.Index,
			Action:         string(step.Action),
			Intent:         step.Intent,
			Preconditions:  conditionViews(step.Preconditions),
			Postconditions: conditionViews(step.Postconditions),
		}
		if step.Value != nil {
			view.Value = *step.Value
		}
		if step.Target != nil {
			view.Target = step.Target.Hint
		}
		views = append(views, view)
	}
	return views
}

func conditionViews(conditions []contract.Condition) []string {
	out := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		out = append(out, fmt.Sprintf("%s:%s", condition.Type, condition.Value))
	}
	return out
}

type pageViewRank struct {
	lastMatches int
	kind        int
	goalMatches int
	confidence  int
	viewport    int
	stableRef   string
}

type rankedBlocker struct {
	value contract.Blocker
	rank  pageViewRank
}

type rankedCandidate struct {
	value contract.ActionCandidate
	rank  pageViewRank
}

type rankedScope struct {
	value contract.StructureNode
	rank  pageViewRank
}

type rankedElement struct {
	value contract.Element
	rank  pageViewRank
}

type pageViewRanking struct {
	goalTokens          map[string]struct{}
	lastTokens          map[string]struct{}
	elementsByRef       map[string]contract.Element
	scopesByRef         map[string]contract.StructureNode
	preferredTargetRefs map[string]bool
	dismissLocators     map[contract.Locator]bool
}

// BuildPageView projects a full observation into the ranked, bounded state
// sent to the model. Resolver code continues to use the original observation.
func BuildPageView(
	observation contract.Observation, goal string, last *CompactResult,
) *PageView {
	view := &PageView{
		URL:              observation.URL,
		Title:            observation.Title,
		Truncated:        observation.Truncated,
		TruncationReason: observation.TruncationReason,
	}
	ranking := newPageViewRanking(observation, goal, last)

	blockers := make([]rankedBlocker, 0, len(observation.Blockers))
	for _, blocker := range observation.Blockers {
		text := ranking.blockerText(blocker)
		blockers = append(blockers, rankedBlocker{
			value: blocker,
			rank: ranking.rank(
				strings.Join([]string{blocker.Ref, blocker.CoversTargetRef, text}, " "),
				text,
				ranking.preferredBlocker(blocker),
				blocker.Confidence,
				true,
				stablePageRef(blocker.Ref, blocker.Kind, blocker.Reason),
			),
		})
	}
	sort.SliceStable(blockers, func(i, j int) bool {
		return pageViewRankBefore(blockers[i].rank, blockers[j].rank)
	})
	omittedBlockers := omittedPageItems(len(blockers), maxPageBlockers)
	for _, item := range blockers[:len(blockers)-omittedBlockers] {
		blocker := item.value
		view.Blockers = append(view.Blockers, BlockerView{
			Kind:              blocker.Kind,
			Ref:               blocker.Ref,
			Confidence:        blocker.Confidence,
			CoversTargetRef:   blocker.CoversTargetRef,
			DismissCandidates: len(blocker.DismissCandidates),
			Reason:            compactText(blocker.Reason, maxPageSummaryText),
		})
	}

	candidates := make([]rankedCandidate, 0, len(observation.ActionCandidates))
	for _, candidate := range observation.ActionCandidates {
		target := ranking.elementsByRef[candidate.TargetRef]
		text := ranking.candidateText(candidate)
		relationValues := []string{candidate.CandidateID, candidate.TargetRef, text}
		for _, relation := range candidate.Relations {
			relationValues = append(relationValues, relation.Ref)
		}
		candidates = append(candidates, rankedCandidate{
			value: candidate,
			rank: ranking.rank(
				strings.Join(relationValues, " "),
				text,
				ranking.preferredCandidate(candidate),
				candidate.Confidence,
				target.VisibleInViewport,
				stablePageRef(candidate.CandidateID, candidate.TargetRef),
			),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return pageViewRankBefore(candidates[i].rank, candidates[j].rank)
	})
	omittedCandidates := omittedPageItems(len(candidates), maxPageActionCandidates)
	for _, item := range candidates[:len(candidates)-omittedCandidates] {
		candidate := item.value
		view.ActionCandidates = append(view.ActionCandidates, ActionCandidateView{
			CandidateID: candidate.CandidateID,
			Kind:        candidate.Kind,
			Action:      candidate.Action,
			TargetRef:   candidate.TargetRef,
			Role:        candidate.Role,
			Name:        candidate.Name,
			Text:        compactText(candidate.Text, maxPageElementText),
			Aliases:     compactStrings(candidate.Aliases, 12, 80),
			Attributes:  compactAttributes(candidate.Attributes, 8, 80),
			Relations:   candidate.Relations,
			Confidence:  candidate.Confidence,
		})
	}

	scopes := make([]rankedScope, 0, len(observation.Structures))
	for _, scope := range observation.Structures {
		if !scope.Visible {
			continue
		}
		text := ranking.scopeText(scope)
		scopes = append(scopes, rankedScope{
			value: scope,
			rank: ranking.rank(
				strings.Join([]string{scope.Ref, scope.ParentRef, text}, " "),
				text,
				isPreferredPageKind(scope.Kind),
				"",
				boundingBoxVisible(scope.BBox),
				stablePageRef(scope.Ref, scope.Kind, scope.FullText),
			),
		})
	}
	sort.SliceStable(scopes, func(i, j int) bool {
		return pageViewRankBefore(scopes[i].rank, scopes[j].rank)
	})
	omittedScopes := omittedPageItems(len(scopes), maxPageScopes)
	for _, item := range scopes[:len(scopes)-omittedScopes] {
		scope := item.value
		view.Scopes = append(view.Scopes, ScopeView{
			Ref:       scope.Ref,
			Kind:      scope.Kind,
			Text:      compactText(scope.FullText, maxPageSummaryText),
			ParentRef: scope.ParentRef,
		})
	}

	elements := make([]rankedElement, 0, len(observation.Elements))
	for _, element := range observation.Elements {
		if element.Visible && element.Enabled {
			text := ranking.elementText(element)
			elements = append(elements, rankedElement{
				value: element,
				rank: ranking.rank(
					strings.Join([]string{element.Ref, element.ParentRef, element.ContainerRef, text}, " "),
					text,
					ranking.preferredElement(element),
					"",
					element.VisibleInViewport,
					stablePageRef(element.Ref, element.Role, element.Name, element.Text),
				),
			})
		}
	}
	sort.SliceStable(elements, func(i, j int) bool {
		return pageViewRankBefore(elements[i].rank, elements[j].rank)
	})
	omittedElements := omittedPageItems(len(elements), maxPageElements)
	for _, item := range elements[:len(elements)-omittedElements] {
		element := item.value
		elementView := ElementView{
			Ref:          element.Ref,
			Tag:          element.Tag,
			Role:         element.Role,
			Name:         element.Name,
			Text:         compactText(element.Text, maxPageElementText),
			ContainerRef: element.ContainerRef,
		}
		if element.Value != nil {
			elementView.Value = *element.Value
		}
		if len(element.Locators) > 0 {
			elementView.LocatorKind = element.Locators[0].Kind
		}
		view.Elements = append(view.Elements, elementView)
	}
	if omittedBlockers+omittedCandidates+omittedScopes+omittedElements > 0 {
		view.Truncated = true
		reason := fmt.Sprintf(
			"page_view_omitted blockers=%d action_candidates=%d scopes=%d elements=%d",
			omittedBlockers,
			omittedCandidates,
			omittedScopes,
			omittedElements,
		)
		if view.TruncationReason == "" {
			view.TruncationReason = reason
		} else {
			view.TruncationReason += "; " + reason
		}
	}
	return view
}

func pageView(observation contract.Observation) *PageView {
	return BuildPageView(observation, "", nil)
}

func newPageViewRanking(
	observation contract.Observation, goal string, last *CompactResult,
) pageViewRanking {
	ranking := pageViewRanking{
		goalTokens:          pageViewTokens(goal),
		lastTokens:          pageViewLastTokens(last),
		elementsByRef:       make(map[string]contract.Element, len(observation.Elements)),
		scopesByRef:         make(map[string]contract.StructureNode, len(observation.Structures)),
		preferredTargetRefs: map[string]bool{},
		dismissLocators:     map[contract.Locator]bool{},
	}
	for _, element := range observation.Elements {
		ranking.elementsByRef[element.Ref] = element
	}
	for _, scope := range observation.Structures {
		ranking.scopesByRef[scope.Ref] = scope
	}
	for _, blocker := range observation.Blockers {
		for _, locator := range blocker.DismissCandidates {
			ranking.dismissLocators[locator] = true
		}
	}
	for _, candidate := range observation.ActionCandidates {
		if ranking.preferredCandidate(candidate) {
			ranking.preferredTargetRefs[candidate.TargetRef] = true
		}
	}
	return ranking
}

func (ranking pageViewRanking) rank(
	relationText string,
	semanticText string,
	preferred bool,
	confidence string,
	viewport bool,
	stableRef string,
) pageViewRank {
	return pageViewRank{
		lastMatches: tokenMatches(ranking.lastTokens, pageViewTokens(relationText)),
		kind:        boolRank(preferred),
		goalMatches: tokenMatches(ranking.goalTokens, pageViewTokens(semanticText)),
		confidence:  confidenceRank(confidence),
		viewport:    boolRank(viewport),
		stableRef:   stableRef,
	}
}

func pageViewRankBefore(left, right pageViewRank) bool {
	switch {
	case left.lastMatches != right.lastMatches:
		return left.lastMatches > right.lastMatches
	case left.kind != right.kind:
		return left.kind > right.kind
	case left.goalMatches != right.goalMatches:
		return left.goalMatches > right.goalMatches
	case left.confidence != right.confidence:
		return left.confidence > right.confidence
	case left.viewport != right.viewport:
		return left.viewport > right.viewport
	default:
		return left.stableRef < right.stableRef
	}
}

func (ranking pageViewRanking) preferredBlocker(blocker contract.Blocker) bool {
	return isDialogPageKind(blocker.Kind) || len(blocker.DismissCandidates) > 0
}

func (ranking pageViewRanking) preferredCandidate(candidate contract.ActionCandidate) bool {
	if normalizedPageKind(candidate.Kind) == "form submit candidate" ||
		isDialogPageKind(candidate.Kind) ||
		ranking.dismissLocators[candidate.Locator] {
		return true
	}
	for _, relation := range candidate.Relations {
		if isDialogPageKind(relation.Type) {
			return true
		}
	}
	target, ok := ranking.elementsByRef[candidate.TargetRef]
	return ok && ranking.scopeHasDialogKind(target.ContainerRef)
}

func (ranking pageViewRanking) preferredElement(element contract.Element) bool {
	return ranking.preferredTargetRefs[element.Ref] ||
		ranking.scopeHasDialogKind(element.ContainerRef)
}

func (ranking pageViewRanking) scopeHasDialogKind(ref string) bool {
	seen := map[string]bool{}
	for ref != "" && !seen[ref] {
		seen[ref] = true
		scope, ok := ranking.scopesByRef[ref]
		if !ok {
			return false
		}
		if isDialogPageKind(scope.Kind) {
			return true
		}
		ref = scope.ParentRef
	}
	return false
}

func (ranking pageViewRanking) blockerText(blocker contract.Blocker) string {
	values := []string{blocker.Kind, blocker.Reason}
	if element, ok := ranking.elementsByRef[blocker.CoversTargetRef]; ok {
		values = append(values, ranking.elementText(element))
	}
	return strings.Join(values, " ")
}

func (ranking pageViewRanking) candidateText(candidate contract.ActionCandidate) string {
	values := []string{
		candidate.Kind,
		candidate.Role,
		candidate.Name,
		candidate.Text,
	}
	values = append(values, candidate.Aliases...)
	for key, value := range candidate.Attributes {
		values = append(values, key, value)
	}
	for _, relation := range candidate.Relations {
		values = append(values, relation.Type, relation.Label)
	}
	if element, ok := ranking.elementsByRef[candidate.TargetRef]; ok {
		values = append(values, ranking.elementText(element))
	}
	return strings.Join(values, " ")
}

func (ranking pageViewRanking) scopeText(scope contract.StructureNode) string {
	var values []string
	seen := map[string]bool{}
	for {
		values = append(values, scope.Kind, scope.Role, scope.FullText)
		for key, value := range scope.Attributes {
			values = append(values, key, value)
		}
		if scope.ParentRef == "" || seen[scope.ParentRef] {
			break
		}
		seen[scope.ParentRef] = true
		parent, ok := ranking.scopesByRef[scope.ParentRef]
		if !ok {
			break
		}
		scope = parent
	}
	return strings.Join(values, " ")
}

func (ranking pageViewRanking) elementText(element contract.Element) string {
	values := []string{
		element.Tag,
		element.Role,
		element.Name,
		element.Text,
		element.OwnText,
		element.FullText,
	}
	if element.Value != nil {
		values = append(values, *element.Value)
	}
	for key, value := range element.Attributes {
		values = append(values, key, value)
	}
	if scope, ok := ranking.scopesByRef[element.ContainerRef]; ok {
		values = append(values, ranking.scopeText(scope))
	}
	return strings.Join(values, " ")
}

func pageViewLastTokens(last *CompactResult) map[string]struct{} {
	if last == nil {
		return nil
	}
	var values []string
	if last.OK {
		values = append(values, last.RelevanceTerms...)
	}
	if last.Failure != nil {
		for _, step := range last.Failure.Steps {
			values = append(values, step.Action)
			if step.Blocker != nil {
				values = append(
					values,
					step.Blocker.Kind,
					step.Blocker.Ref,
					step.Blocker.CoversTargetRef,
				)
			}
			if step.HitTest != nil {
				values = append(values, step.HitTest.TargetRef, step.HitTest.HitRef, step.HitTest.BlockerKind)
			}
		}
	}
	tokens := pageViewTokens(strings.Join(values, " "))
	for token := range tokens {
		if pageViewResultStopToken(token) {
			delete(tokens, token)
		}
	}
	return tokens
}

func pageViewTokens(value string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var current []rune
	currentIsHan := false
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens[string(current)] = struct{}{}
		if currentIsHan {
			for index := 0; index+1 < len(current); index++ {
				tokens[string(current[index:index+2])] = struct{}{}
			}
		}
		current = current[:0]
	}
	for _, char := range strings.ToLower(value) {
		isHan := unicode.Is(unicode.Han, char)
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			if len(current) > 0 && currentIsHan != isHan {
				flush()
			}
			currentIsHan = isHan
			current = append(current, char)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func stepRelevanceTerms(step contract.Step) []string {
	var values []string
	if step.Target != nil {
		values = append(values, step.Target.Hint)
		if spec := step.Target.Spec; spec != nil {
			values = append(
				values,
				spec.Object.Role,
				spec.Object.Text,
				spec.Object.Name,
			)
			values = append(values, spec.Object.Aliases...)
			if spec.Scope != nil {
				values = append(values, spec.Scope.Kind, spec.Scope.ContainsText)
			}
			values = append(values, spec.Relation, spec.Role, spec.Text, spec.Name)
			values = append(values, spec.Aliases...)
		}
	}
	for _, condition := range step.Postconditions {
		values = append(values, condition.Value)
	}
	return compactStrings(values, maxResultRelevanceTerms, maxResultRelevanceText)
}

func pageViewResultStopToken(token string) bool {
	switch token {
	case "action", "click", "current", "error", "failed", "failure", "input",
		"on", "page", "recorded", "step", "target":
		return true
	default:
		return false
	}
}

func tokenMatches(wanted, available map[string]struct{}) int {
	matches := 0
	for token := range wanted {
		if _, ok := available[token]; ok {
			matches++
		}
	}
	return matches
}

func confidenceRank(confidence string) int {
	switch normalize(confidence) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func isPreferredPageKind(kind string) bool {
	normalized := normalizedPageKind(kind)
	return normalized == "form" || isDialogPageKind(normalized)
}

func isDialogPageKind(kind string) bool {
	normalized := normalizedPageKind(kind)
	return strings.Contains(normalized, "dialog") ||
		strings.Contains(normalized, "modal") ||
		strings.Contains(normalized, "dismiss")
}

func normalizedPageKind(kind string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ")
	return normalize(replacer.Replace(kind))
}

func boundingBoxVisible(box contract.BoundingBox) bool {
	return box.Width > 0 && box.Height > 0 && box.X+box.Width > 0 && box.Y+box.Height > 0
}

func stablePageRef(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func omittedPageItems(count, limit int) int {
	if count <= limit {
		return 0
	}
	return count - limit
}

func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

func compactStrings(values []string, limit int, textLimit int) []string {
	out := make([]string, 0, limit)
	seen := map[string]bool{}
	for _, value := range values {
		if len(out) >= limit {
			break
		}
		value = compactText(value, textLimit)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func compactAttributes(values map[string]string, limit int, textLimit int) map[string]string {
	if len(values) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"id": true, "name": true, "type": true, "title": true,
		"placeholder": true, "aria-label": true, "data-testid": true,
	}
	out := map[string]string{}
	for key, value := range values {
		if len(out) >= limit {
			break
		}
		if !allowed[key] {
			continue
		}
		if compacted := compactText(value, textLimit); compacted != "" {
			out[key] = compacted
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactText(value string, limit int) string {
	normalized := strings.Join(strings.Fields(value), " ")
	runes := []rune(normalized)
	if len(runes) <= limit {
		return normalized
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func observationHasText(observation contract.Observation, text string) bool {
	needle := normalize(text)
	for _, element := range observation.Elements {
		if !element.Visible {
			continue
		}
		if strings.Contains(normalize(element.Text), needle) ||
			strings.Contains(normalize(element.Name), needle) {
			return true
		}
	}
	return false
}

func dryRunFailure(result contract.ExecutionResult) *DryRunFailure {
	failure := &DryRunFailure{
		ExecutionID: result.ExecutionID,
		Status:      string(result.Status),
		FinalURL:    result.FinalURL,
		CurrentURL:  result.FinalURL,
	}
	if result.Error != nil {
		failure.Error = fmt.Sprintf("%s: %s", result.Error.Kind, result.Error.Message)
	}
	for _, step := range result.Steps {
		if step.Status == "passed" {
			continue
		}
		item := DryRunStep{
			Index:  step.Index,
			Action: string(step.Action),
			Status: step.Status,
			URL:    step.URLAfter,
		}
		if step.Error != nil {
			item.Error = fmt.Sprintf("%s: %s", step.Error.Kind, step.Error.Message)
		}
		item.Blocker = step.Blocker
		item.HitTest = step.HitTest
		item.Recovery = step.Recovery
		for _, condition := range step.Conditions {
			if condition.Satisfied {
				continue
			}
			detail := ""
			if condition.Detail != nil {
				detail = *condition.Detail
			}
			item.Unsatisfied = append(
				item.Unsatisfied,
				fmt.Sprintf("%s(%s):%s %s", condition.Type, condition.Phase, condition.Value, detail),
			)
		}
		failure.Steps = append(failure.Steps, item)
		if failure.FailedStep == nil {
			clone := item
			failure.FailedStep = &clone
			if clone.URL != "" {
				failure.CurrentURL = clone.URL
			}
			failure.RepairHints = repairHintsForStep(step)
		}
	}
	return failure
}

func repairHintsForStep(step contract.StepResult) []string {
	hints := []string{"reobserve current page before retrying"}
	if step.Error != nil {
		switch step.Error.Kind {
		case contract.SignalBlockedByAuth:
			hints = append(hints, "ask the user to clear the auth wall, then open_page to reobserve")
		case contract.SignalBlockedByCaptcha:
			hints = append(hints, "ask the user to complete the captcha, then open_page to reobserve")
		case contract.SignalBlockedByDialog, contract.SignalBlockedByOverlay,
			contract.SignalBlockedByInterstitial, contract.SignalBlockedByCookieBanner:
			hints = append(hints, "try dismiss_dialog or reobserve after the safe recovery attempt")
		case contract.SignalBlockedByLoading:
			hints = append(hints, "wait for loading to disappear, then open_page to reobserve")
		case contract.SignalTargetNotFound:
			hints = append(hints, "narrow target scope or use an alias from the current observation")
		case contract.SignalConditionUnmet:
			hints = append(hints, "rebuild the removed failing tail with a different expectation")
		case contract.SignalStepTimeout:
			hints = append(hints, "scroll target into view or wait for a stable visible element")
		default:
			hints = append(hints, "ask the user only if required information is missing")
		}
	}
	return hints
}

func failure(code, detail string) Result {
	return Result{OK: false, Error: code, Detail: detail}
}

func workerFailure(code string, err error) Result {
	return Result{OK: false, Error: code, Detail: err.Error()}
}
