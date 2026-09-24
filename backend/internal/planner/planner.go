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
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

// maxPageElements 限制回给模型的元素数量，控制 token。
const maxPageElements = 60

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
	snapshot := StateSnapshot{
		Version:  1,
		Steps:    stepViews(p.steps),
		Failures: append([]FailureSignature{}, p.failures...),
	}
	if p.hasPage {
		snapshot.Page = pageView(p.observation)
	}
	if lastResult != nil {
		snapshot.LastResult = &CompactResult{
			OK:      lastResult.OK,
			Summary: lastResult.Summary,
			Warning: lastResult.Warning,
			Error:   lastResult.Error,
			Detail:  lastResult.Detail,
			Failure: lastResult.Failure,
		}
	}
	return snapshot
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
	targetKey := failureTargetKey(selectedCandidateID, spec)
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

func pageView(observation contract.Observation) *PageView {
	view := &PageView{
		URL:              observation.URL,
		Title:            observation.Title,
		Truncated:        observation.Truncated,
		TruncationReason: observation.TruncationReason,
	}
	for _, blocker := range observation.Blockers {
		view.Blockers = append(view.Blockers, BlockerView{
			Kind:              blocker.Kind,
			Ref:               blocker.Ref,
			Confidence:        blocker.Confidence,
			CoversTargetRef:   blocker.CoversTargetRef,
			DismissCandidates: len(blocker.DismissCandidates),
			Reason:            compactText(blocker.Reason, 160),
		})
	}
	const maxActionCandidates = 30
	for _, candidate := range observation.ActionCandidates {
		if len(view.ActionCandidates) >= maxActionCandidates {
			view.Truncated = true
			if view.TruncationReason == "" {
				view.TruncationReason = fmt.Sprintf("action_candidate_view_limit=%d", maxActionCandidates)
			}
			break
		}
		view.ActionCandidates = append(view.ActionCandidates, ActionCandidateView{
			CandidateID: candidate.CandidateID,
			Kind:        candidate.Kind,
			Action:      candidate.Action,
			TargetRef:   candidate.TargetRef,
			Role:        candidate.Role,
			Name:        candidate.Name,
			Text:        compactText(candidate.Text, 120),
			Aliases:     compactStrings(candidate.Aliases, 12, 80),
			Attributes:  compactAttributes(candidate.Attributes, 8, 80),
			Relations:   candidate.Relations,
			Confidence:  candidate.Confidence,
		})
	}
	const maxScopes = 30
	for _, scope := range observation.Structures {
		if !scope.Visible {
			continue
		}
		if len(view.Scopes) >= maxScopes {
			view.Truncated = true
			if view.TruncationReason == "" {
				view.TruncationReason = fmt.Sprintf("scope_view_limit=%d", maxScopes)
			}
			break
		}
		view.Scopes = append(view.Scopes, ScopeView{
			Ref:       scope.Ref,
			Kind:      scope.Kind,
			Text:      compactText(scope.FullText, 160),
			ParentRef: scope.ParentRef,
		})
	}
	actionable := make([]contract.Element, 0, len(observation.Elements))
	for _, element := range observation.Elements {
		if element.Visible && element.Enabled {
			actionable = append(actionable, element)
		}
	}
	if len(actionable) > maxPageElements {
		view.Truncated = true
		actionable = actionable[:maxPageElements]
	}
	for _, element := range actionable {
		elementView := ElementView{
			Ref:          element.Ref,
			Tag:          element.Tag,
			Role:         element.Role,
			Name:         element.Name,
			Text:         compactText(element.Text, 120),
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
	return view
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
	if len(normalized) <= limit {
		return normalized
	}
	return normalized[:limit] + "..."
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
