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
	ctx context.Context, hint, intent string, expects contract.Expects,
) (Result, error) {
	return p.act(ctx, contract.ActionClick, hint, nil, intent, expects)
}

func (p *Planner) input(
	ctx context.Context, hint, value, intent string, expects contract.Expects,
) (Result, error) {
	return p.act(ctx, contract.ActionInput, hint, &value, intent, expects)
}

func (p *Planner) act(
	ctx context.Context,
	action contract.Action,
	hint string,
	value *string,
	intent string,
	expects contract.Expects,
) (Result, error) {
	if !p.hasPage {
		return failure("no_observation", "no page has been observed yet; call open_page first"), nil
	}
	element, locator, candidates, err := resolve(hint, p.observation.Elements)
	if err != nil {
		return Result{
			OK:         false,
			Error:      errorCode(err),
			Detail:     err.Error(),
			Page:       pageView(p.observation),
			Candidates: candidates,
		}, nil
	}
	target := contract.Target{
		Hint:    hint,
		Locator: locator,
		Grounding: contract.Grounding{
			ObservationID: p.observation.ObservationID,
			PageStateID:   p.observation.PageStateID,
			CandidateID:   candidateID(element, locator),
			PageURL:       p.observation.URL,
		},
	}
	step, err := contract.DeriveActionStep(
		len(p.steps), action, intent, target, value, p.observation.URL, expects,
	)
	if err != nil {
		return failure("step_rejected", err.Error()), nil
	}
	// 先真的执行动作，成功后才记录步骤：动作失败不该留下一条假步骤。
	request := contract.ActRequest{Action: action, Locator: locator}
	if value != nil {
		request.Value = *value
	}
	observation, err := p.client.Act(ctx, p.browserSessionID, request)
	if err != nil {
		return workerFailure("action_failed", err), nil
	}
	p.steps = append(p.steps, step)
	p.observation = observation
	result := Result{
		OK:      true,
		Summary: fmt.Sprintf("recorded %s step %d on %q", action, step.Index, element.Name),
		Page:    pageView(observation),
		Steps:   stepViews(p.steps),
	}
	return result, nil
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

func (p *Planner) dropLastStep() (Result, error) {
	if len(p.steps) == 0 {
		return failure("no_steps", "there is no step to drop"), nil
	}
	dropped := p.steps[len(p.steps)-1]
	p.steps = p.steps[:len(p.steps)-1]
	return Result{
		OK:      true,
		Summary: fmt.Sprintf("dropped step %d (%s %s)", dropped.Index, dropped.Action, dropped.Intent),
		Steps:   stepViews(p.steps),
	}, nil
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
		return contract.Case{}, result, Result{
			OK:      false,
			Error:   "dry_run_failed",
			Detail:  "the authored case did not pass a full dry run; fix the failing step and call finish_case again",
			Failure: dryRunFailure(result),
			Steps:   stepViews(p.steps),
		}, nil
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
		URL:   observation.URL,
		Title: observation.Title,
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
			Ref:  element.Ref,
			Tag:  element.Tag,
			Role: element.Role,
			Name: element.Name,
			Text: element.Text,
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
	}
	return failure
}

func failure(code, detail string) Result {
	return Result{OK: false, Error: code, Detail: detail}
}

func workerFailure(code string, err error) Result {
	return Result{OK: false, Error: code, Detail: err.Error()}
}
