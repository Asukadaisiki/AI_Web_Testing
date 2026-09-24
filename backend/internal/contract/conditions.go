package contract

import (
	"errors"
	"fmt"
	"strings"
)

// ConditionType 是全部合法的条件类型。
type ConditionType string

const (
	CondURLContains     ConditionType = "url_contains"
	CondTextVisible     ConditionType = "text_visible"
	CondTextGone        ConditionType = "text_gone"
	CondURLChanges      ConditionType = "url_changes"
	CondValueEquals     ConditionType = "value_equals"
	CondElementState    ConditionType = "element_state"
	CondAttributeEquals ConditionType = "attribute_equals"
	CondCountEquals     ConditionType = "count_equals"
)

type phaseSet struct {
	pre  bool
	post bool
}

// conditionPhases 是条件阶段表的唯一权威实现（对应 CONTRACT.md §2.2）。
//
// 之所以要有阶段表：执行器在动作前拍快照并立刻评估前置条件，此时还没有任何
// 变化与事件，因此"变化型/事件型"条件作为前置条件在结构上永远为假。旧版把
// 这类条件写成前置条件、又要求动作步骤必须有前置条件，直接把作者逼进死路。
var conditionPhases = map[ConditionType]phaseSet{
	CondURLContains:     {pre: true, post: true},
	CondTextVisible:     {pre: true, post: true},
	CondTextGone:        {pre: true, post: true},
	CondURLChanges:      {post: true},
	CondValueEquals:     {post: true},
	CondElementState:    {pre: true, post: true},
	CondAttributeEquals: {post: true},
	CondCountEquals:     {post: true},
}

// conditionOrder 固定遍历顺序，保证提示词与错误信息稳定。
var conditionOrder = []ConditionType{
	CondURLContains,
	CondTextVisible,
	CondTextGone,
	CondURLChanges,
	CondValueEquals,
	CondElementState,
	CondAttributeEquals,
	CondCountEquals,
}

var actionOrder = []Action{
	ActionGoto,
	ActionClick,
	ActionInput,
	ActionSelect,
	ActionCheck,
	ActionUncheck,
	ActionScrollIntoView,
	ActionHover,
	ActionDismissDialog,
	ActionUploadFile,
	ActionAssertText,
	ActionAssertURL,
	ActionAssertElement,
	ActionAssertAttribute,
	ActionAssertCount,
}

// ConditionAllowed 判断某条件类型能否出现在指定阶段。
func ConditionAllowed(conditionType ConditionType, phase Phase) bool {
	set, known := conditionPhases[conditionType]
	if !known {
		return false
	}
	if phase == PhasePre {
		return set.pre
	}
	return set.post
}

// ConditionTypes 返回全部条件类型（固定顺序）。
func ConditionTypes() []ConditionType {
	out := make([]ConditionType, len(conditionOrder))
	copy(out, conditionOrder)
	return out
}

// PreconditionTypes 返回可作为前置条件的类型，即状态事实。
func PreconditionTypes() []ConditionType {
	out := make([]ConditionType, 0, len(conditionOrder))
	for _, conditionType := range conditionOrder {
		if conditionPhases[conditionType].pre {
			out = append(out, conditionType)
		}
	}
	return out
}

// ActionNames 返回全部动作名（固定顺序）。
func ActionNames() []string {
	out := make([]string, 0, len(actionOrder))
	for _, action := range actionOrder {
		out = append(out, string(action))
	}
	return out
}

func conditionTypeNames() []string {
	out := make([]string, 0, len(conditionOrder))
	for _, conditionType := range conditionOrder {
		out = append(out, string(conditionType))
	}
	return out
}

// PreconditionTypeNames 供错误信息与提示词复用。
func PreconditionTypeNames() []string {
	types := PreconditionTypes()
	out := make([]string, 0, len(types))
	for _, conditionType := range types {
		out = append(out, string(conditionType))
	}
	return out
}

// ConditionSemantics 是给提示词用的语义说明，避免提示词与代码各写一份。
var ConditionSemantics = map[ConditionType]string{
	CondURLContains:     "current page url contains the value",
	CondTextVisible:     "an element whose visible text contains the value exists",
	CondTextGone:        "no visible element whose text contains the value exists",
	CondURLChanges:      "the page url differs from the url before this step (postcondition only)",
	CondValueEquals:     "the target element's value equals the value (postcondition only)",
	CondElementState:    "the target element state matches visible, hidden, enabled, disabled, checked, or unchecked",
	CondAttributeEquals: "the target element attribute/value assertion matches attr=value",
	CondCountEquals:     "the target locator count equals the numeric value",
}

// Expects 是模型能表达的期望。模型不写条件，只写期望。
type Expects struct {
	Text      *string `json:"text,omitempty"`
	Gone      *string `json:"gone,omitempty"`
	URL       *string `json:"url,omitempty"`
	Value     *string `json:"value,omitempty"`
	Element   *string `json:"element,omitempty"`
	Attribute *string `json:"attribute,omitempty"`
	Count     *string `json:"count,omitempty"`
}

// IsZero 表示模型没有声明任何期望。
func (e Expects) IsZero() bool {
	return e.Text == nil && e.Gone == nil && e.URL == nil && e.Value == nil &&
		e.Element == nil && e.Attribute == nil && e.Count == nil
}

// Conditions 把期望翻译成后置条件。至少一个，否则报错。
func (e Expects) Conditions() ([]Condition, error) {
	conditions := make([]Condition, 0, 4)
	appendCondition := func(conditionType ConditionType, value string) {
		conditions = append(conditions, Condition{
			Type:      conditionType,
			Value:     value,
			TimeoutMS: DefaultConditionTimeoutMS,
		})
	}
	if e.Text != nil {
		appendCondition(CondTextVisible, *e.Text)
	}
	if e.Gone != nil {
		appendCondition(CondTextGone, *e.Gone)
	}
	if e.URL != nil {
		appendCondition(CondURLContains, *e.URL)
	}
	if e.Value != nil {
		appendCondition(CondValueEquals, *e.Value)
	}
	if e.Element != nil {
		appendCondition(CondElementState, *e.Element)
	}
	if e.Attribute != nil {
		appendCondition(CondAttributeEquals, *e.Attribute)
	}
	if e.Count != nil {
		appendCondition(CondCountEquals, *e.Count)
	}
	if len(conditions) == 0 {
		return nil, errors.New(
			"an action step must declare at least one expectation: expect_text, expect_gone, expect_url or expect_value",
		)
	}
	return conditions, nil
}

// DeriveGotoStep 派生导航步骤：无前置条件，后置条件为页面锚点。
func DeriveGotoStep(index int, intent, rawURL string) (Step, error) {
	if !isAbsoluteURL(rawURL) {
		return Step{}, fmt.Errorf("open_page url must be absolute, got %q", rawURL)
	}
	step := Step{
		Index:         index,
		Action:        ActionGoto,
		Intent:        intent,
		Value:         &rawURL,
		TimeoutMS:     DefaultStepTimeoutMS,
		Preconditions: []Condition{},
		Postconditions: []Condition{{
			Type:      CondURLContains,
			Value:     PageAnchor(rawURL),
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
	}
	if strings.TrimSpace(intent) == "" {
		step.Intent = "open " + rawURL
	}
	return finishDerivedStep(step)
}

// DeriveActionStep 派生带 target 的动作/断言步骤。
//
// 前置条件来自"最近一次观测所在页面的锚点"：前一步的后置条件已保证到达该页，
// 因此这个前置条件天然可满足，不会出现"条件永远判不过"。
func DeriveActionStep(
	index int,
	action Action,
	intent string,
	target Target,
	value *string,
	submit bool,
	observedPageURL string,
	expects Expects,
) (Step, error) {
	if !actionRequiresTarget(action) {
		return Step{}, fmt.Errorf("derive action step: unsupported action %q", action)
	}
	anchor := PageAnchor(observedPageURL)
	if anchor == "" {
		return Step{}, errors.New("derive action step: the grounding observation has no page url")
	}
	postconditions, err := expects.Conditions()
	if err != nil {
		return Step{}, err
	}
	step := Step{
		Index:  index,
		Action: action,
		Intent: intent,
		Target: &target,
		Preconditions: []Condition{{
			Type:      CondURLContains,
			Value:     anchor,
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
		Postconditions: postconditions,
		TimeoutMS:      DefaultStepTimeoutMS,
	}
	if action == ActionInput || action == ActionSelect || action == ActionUploadFile {
		if value == nil {
			empty := ""
			value = &empty
		}
		step.Value = value
	}
	// 无条件带上 submit，合法性交给 ValidateStep 判：只允许出现在 input 上。
	// 不在这里静默丢弃——静默丢弃会把调用方的错误藏起来，而"构建即校验"的意义
	// 就是让派生逻辑自身的问题在作者态暴露。
	step.Submit = submit
	return finishDerivedStep(step)
}

func actionRequiresTarget(action Action) bool {
	switch action {
	case ActionClick, ActionInput, ActionSelect, ActionCheck, ActionUncheck,
		ActionScrollIntoView, ActionHover, ActionDismissDialog, ActionUploadFile,
		ActionAssertElement, ActionAssertAttribute, ActionAssertCount:
		return true
	default:
		return false
	}
}

// DeriveAssertTextStep 派生页面级文本断言。
func DeriveAssertTextStep(index int, intent, text, observedPageURL string) (Step, error) {
	if text == "" {
		return Step{}, errors.New("assert_text requires a non-empty text")
	}
	anchor := PageAnchor(observedPageURL)
	if anchor == "" {
		return Step{}, errors.New("assert_text requires a grounded page url")
	}
	step := Step{
		Index:  index,
		Action: ActionAssertText,
		Intent: intent,
		Value:  &text,
		Preconditions: []Condition{{
			Type:      CondURLContains,
			Value:     anchor,
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
		Postconditions: []Condition{{
			Type:      CondTextVisible,
			Value:     text,
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
		TimeoutMS: DefaultStepTimeoutMS,
	}
	return finishDerivedStep(step)
}

// DeriveAssertURLStep 派生页面级 URL 断言。
func DeriveAssertURLStep(index int, intent, contains, observedPageURL string) (Step, error) {
	if contains == "" {
		return Step{}, errors.New("assert_url requires a non-empty substring")
	}
	anchor := PageAnchor(observedPageURL)
	if anchor == "" {
		return Step{}, errors.New("assert_url requires a grounded page url")
	}
	step := Step{
		Index:  index,
		Action: ActionAssertURL,
		Intent: intent,
		Value:  &contains,
		Preconditions: []Condition{{
			Type:      CondURLContains,
			Value:     anchor,
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
		Postconditions: []Condition{{
			Type:      CondURLContains,
			Value:     contains,
			TimeoutMS: DefaultConditionTimeoutMS,
		}},
		TimeoutMS: DefaultStepTimeoutMS,
	}
	return finishDerivedStep(step)
}

// finishDerivedStep 是"构建即校验"：派生结果立即过一遍同一个校验器，
// 派生逻辑自身出错会在作者态就暴露，而不是等到执行期。
func finishDerivedStep(step Step) (Step, error) {
	if step.Intent == "" {
		return Step{}, errors.New("step intent is required")
	}
	if err := ValidateStep(step.Index, step); err != nil {
		return Step{}, fmt.Errorf("derived step failed the contract: %w", err)
	}
	return step, nil
}
