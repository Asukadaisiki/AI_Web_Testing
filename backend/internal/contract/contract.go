// Package contract 是 v2 闭环里所有跨进程数据的唯一权威定义。
//
// 镜像位置（改这里必须同步改）：
//   - v2/CONTRACT.md        人读版
//   - v2/worker/loop_worker/contracts.py
//   - v2/web/src/api.ts
//
// 设计要点：case 不由模型书写，而由本包的 Derive* 函数根据工具调用构建；
// 构建完立即调用同一个校验函数，因此"生成的 DSL 过不了校验"在结构上不可能发生。
// 校验失败一律返回带错误码的 *Violation，模型据此改口重试。
package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// CaseVersion 是 case 工件的版本标识。
const CaseVersion = "loop.case.v1"

// 缺省超时（毫秒）。
//
// 这两个数是按真实站点的实测值定的，不是拍脑袋：冷启动（干跑用的是全新 context，
// 没有任何缓存）打开 automationexercise.com 的 domcontentloaded 实测 5.0–6.1s，
// 站内跳转 /products 实测 6.3s。原来的 5000/3000 正好卡在真实耗时上，导致
// **干跑永远不可能通过**——而干跑不过，case 就永远建不出来，整条闭环死在这里。
//
// 宁可慢也不要假红：超时给足，让失败是真的失败。
const (
	DefaultConditionTimeoutMS = 10000
	DefaultStepTimeoutMS      = 20000
)

// 错误码：模型与跨语言一致性测试都依赖这些字符串，不要随手改。
const (
	CodeInvalidJSON             = "case_invalid_json"
	CodeVersionMismatch         = "case_version_mismatch"
	CodeNameRequired            = "case_name_required"
	CodeEmptySteps              = "case_empty_steps"
	CodeNotGotoFirst            = "case_not_goto_first"
	CodeStepIndexMismatch       = "step_index_mismatch"
	CodeIntentRequired          = "step_intent_required"
	CodeUnknownAction           = "step_unknown_action"
	CodeMissingValue            = "step_missing_value"
	CodeUnexpectedValue         = "step_unexpected_value"
	CodeMissingTarget           = "step_missing_target"
	CodeUnexpectedTarget        = "step_unexpected_target"
	CodeTargetUngrounded        = "step_target_ungrounded"
	CodeInvalidLocator          = "locator_invalid"
	CodeMissingPrecondition     = "step_missing_precondition"
	CodeMissingPostcondition    = "step_missing_postcondition"
	CodeGotoPreconditionForbid  = "goto_precondition_forbidden"
	CodeUnknownConditionType    = "condition_unknown_type"
	CodeConditionPhase          = "condition_phase"
	CodeConditionMissingValue   = "condition_missing_value"
	CodeConditionMissingTimeout = "condition_missing_timeout"
	CodeURLNotAbsolute          = "goto_value_not_absolute"
	CodeUnexpectedSubmit        = "step_unexpected_submit"
)

// Violation 是结构化契约违规。Code 稳定可机读，Message 面向人与模型。
type Violation struct {
	Code    string `json:"code"`
	Step    int    `json:"step"`
	Message string `json:"message"`
}

func (v *Violation) Error() string { return v.Message }

// CodeOf 取出错误里的错误码，非契约错误返回空串。
func CodeOf(err error) string {
	var violation *Violation
	if errors.As(err, &violation) {
		return violation.Code
	}
	return ""
}

func violation(code string, step int, format string, args ...any) *Violation {
	return &Violation{Code: code, Step: step, Message: fmt.Sprintf(format, args...)}
}

// Action 是执行器支持的全部动作，只有这五个。
type Action string

const (
	ActionGoto            Action = "goto"
	ActionClick           Action = "click"
	ActionInput           Action = "input"
	ActionSelect          Action = "select"
	ActionCheck           Action = "check"
	ActionUncheck         Action = "uncheck"
	ActionScrollIntoView  Action = "scroll_into_view"
	ActionHover           Action = "hover"
	ActionDismissDialog   Action = "dismiss_dialog"
	ActionUploadFile      Action = "upload_file"
	ActionAssertText      Action = "assert_text"
	ActionAssertURL       Action = "assert_url"
	ActionAssertElement   Action = "assert_element"
	ActionAssertAttribute Action = "assert_attribute"
	ActionAssertCount     Action = "assert_count"
)

// Phase 是条件的评估阶段。
type Phase string

const (
	PhasePre  Phase = "pre"
	PhasePost Phase = "post"
)

// Locator 描述一个定位器。观测阶段输出的每一条都必须当场验证过。
type Locator struct {
	Kind       string `json:"kind"` // role | text | css
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Exact      bool   `json:"exact,omitempty"`
	Text       string `json:"text,omitempty"`
	CSS        string `json:"css,omitempty"`
	MatchCount int    `json:"match_count"`
}

// Describe 是给错误信息与报告用的可读形式，与 Python 侧 locators.describe 对齐。
func (l Locator) Describe() string {
	switch l.Kind {
	case "role":
		return fmt.Sprintf("role=%s name=%q exact=%t", l.Role, l.Name, l.Exact)
	case "text":
		return fmt.Sprintf("text=%q exact=%t", l.Text, l.Exact)
	case "css":
		return fmt.Sprintf("css=%q", l.CSS)
	default:
		return fmt.Sprintf("kind=%q", l.Kind)
	}
}

// Grounding 记录定位器来自哪一次观测，保证 case 可追溯。
type Grounding struct {
	ObservationID string `json:"observation_id"`
	PageStateID   string `json:"page_state_id"`
	CandidateID   string `json:"candidate_id"`
	PageURL       string `json:"page_url"`
}

// Target 是 click / input 的作用对象，必须已接地。
type Target struct {
	Hint      string      `json:"hint"`
	Locator   Locator     `json:"locator"`
	Grounding Grounding   `json:"grounding"`
	Spec      *TargetSpec `json:"spec,omitempty"`
}

// Condition 是一个可判定的状态或变化断言。
type Condition struct {
	Type      ConditionType `json:"type"`
	Value     string        `json:"value"`
	TimeoutMS int           `json:"timeout_ms"`
}

// Step 是 case 里的一步。
type Step struct {
	Index  int     `json:"index"`
	Action Action  `json:"action"`
	Intent string  `json:"intent"`
	Value  *string `json:"value,omitempty"`
	// Submit 只对 input 有意义：填完之后按回车提交。
	//
	// 真实站点上的搜索提交控件常常是纯图标按钮——可访问名只有一个私有区字形
	// （例如 "\uf002"），text 也是同一个不可见码位，模型不可能用任何 hint 指到它。
	// 回车不需要指到任何控件，是表单的原生提交方式，因此把它做成契约的一部分。
	//
	// 实测边界：只对"表单能被回车提交"的站点有效。若站点的提交控件是
	// `type="button"` + JS（automationexercise.com 的 #submit_search 就是），
	// 回车与 form.requestSubmit() 都不提交，唯一出路是能指到那个按钮本身——
	// 那是别名匹配面的活，尚未实现。
	Submit         bool        `json:"submit,omitempty"`
	Target         *Target     `json:"target,omitempty"`
	Preconditions  []Condition `json:"preconditions"`
	Postconditions []Condition `json:"postconditions"`
	TimeoutMS      int         `json:"timeout_ms"`
}

// Case 是唯一可执行的工件形态。
type Case struct {
	CaseVersion string `json:"case_version"`
	Name        string `json:"name"`
	Goal        string `json:"goal"`
	BaseURL     string `json:"base_url"`
	Steps       []Step `json:"steps"`
}

// Validate 是 case 的唯一校验入口。返回值已归一化（补缺省超时、重排 index）。
func Validate(raw []byte) (Case, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed Case
	if err := decoder.Decode(&parsed); err != nil {
		return Case{}, violation(CodeInvalidJSON, -1, "case is not valid JSON for %s: %v", CaseVersion, err)
	}
	if err := parsed.Validate(); err != nil {
		return Case{}, err
	}
	return parsed, nil
}

// Validate 校验并归一化一个已解析的 case。
func (c *Case) Validate() error {
	normalize(c)
	if c.CaseVersion != CaseVersion {
		return violation(
			CodeVersionMismatch, -1,
			"case.case_version must be %q, got %q", CaseVersion, c.CaseVersion,
		)
	}
	if strings.TrimSpace(c.Name) == "" {
		return violation(CodeNameRequired, -1, "case.name is required")
	}
	if len(c.Steps) == 0 {
		return violation(CodeEmptySteps, -1, "case.steps must contain at least one step")
	}
	if c.Steps[0].Action != ActionGoto {
		return violation(
			CodeNotGotoFirst, 0,
			"case.steps[0] must be a goto step, got %q: the executor never pre-navigates for the first step",
			c.Steps[0].Action,
		)
	}
	for index := range c.Steps {
		if err := ValidateStep(index, c.Steps[index]); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStep 校验单步，错误信息始终带步号，便于模型据此改口。
func ValidateStep(index int, step Step) error {
	if step.Index != index {
		return violation(
			CodeStepIndexMismatch, index,
			"case.steps[%d].index must be %d, got %d", index, index, step.Index,
		)
	}
	if strings.TrimSpace(step.Intent) == "" {
		return violation(CodeIntentRequired, index, "case.steps[%d].intent is required", index)
	}
	switch step.Action {
	case ActionGoto:
		if step.Target != nil {
			return violation(CodeUnexpectedTarget, index, "case.steps[%d] goto must not carry a target", index)
		}
		if step.Value == nil || strings.TrimSpace(*step.Value) == "" {
			return violation(CodeMissingValue, index, "case.steps[%d] goto requires an absolute url in value", index)
		}
		if !isAbsoluteURL(*step.Value) {
			return violation(
				CodeURLNotAbsolute, index,
				"case.steps[%d] goto value must be an absolute url, got %q", index, *step.Value,
			)
		}
		if len(step.Preconditions) != 0 {
			return violation(
				CodeGotoPreconditionForbid, index,
				"case.steps[%d] goto must not declare preconditions: navigation starts from about:blank, so no precondition can hold",
				index,
			)
		}
	case ActionClick, ActionInput, ActionSelect, ActionCheck, ActionUncheck, ActionScrollIntoView,
		ActionHover, ActionDismissDialog, ActionUploadFile, ActionAssertElement, ActionAssertAttribute, ActionAssertCount:
		if step.Target == nil {
			return violation(
				CodeMissingTarget, index,
				"case.steps[%d] %s requires a grounded target", index, step.Action,
			)
		}
		if err := validateTarget(index, *step.Target); err != nil {
			return err
		}
		if step.Action == ActionClick && step.Value != nil {
			return violation(CodeUnexpectedValue, index, "case.steps[%d] click must not carry a value", index)
		}
		if (step.Action == ActionInput || step.Action == ActionSelect || step.Action == ActionUploadFile) && step.Value == nil {
			return violation(
				CodeMissingValue, index,
				"case.steps[%d] %s requires a value (empty string is allowed)", index, step.Action,
			)
		}
		if step.Action != ActionInput && step.Action != ActionSelect && step.Action != ActionUploadFile && step.Value != nil {
			return violation(CodeUnexpectedValue, index, "case.steps[%d] %s must not carry a value", index, step.Action)
		}
		if len(step.Preconditions) == 0 {
			return violation(
				CodeMissingPrecondition, index,
				"case.steps[%d] %s requires at least one precondition", index, step.Action,
			)
		}
	case ActionAssertText, ActionAssertURL:
		if step.Target != nil {
			return violation(
				CodeUnexpectedTarget, index,
				"case.steps[%d] %s must not carry a target: it asserts about the page", index, step.Action,
			)
		}
		if step.Value == nil || *step.Value == "" {
			return violation(CodeMissingValue, index, "case.steps[%d] %s requires a value", index, step.Action)
		}
		if len(step.Preconditions) == 0 {
			return violation(
				CodeMissingPrecondition, index,
				"case.steps[%d] %s requires at least one precondition", index, step.Action,
			)
		}
	default:
		return violation(
			CodeUnknownAction, index,
			"case.steps[%d] unknown action %q; allowed actions: %s", index, step.Action, strings.Join(ActionNames(), ", "),
		)
	}
	// submit 只对 input 有意义。放在 switch 之后：未知 action 仍应先报 unknown_action。
	if step.Submit && step.Action != ActionInput {
		return violation(
			CodeUnexpectedSubmit, index,
			"case.steps[%d] only input may carry submit, got %s", index, step.Action,
		)
	}
	if len(step.Postconditions) == 0 {
		return violation(
			CodeMissingPostcondition, index,
			"case.steps[%d] %s requires at least one postcondition", index, step.Action,
		)
	}
	for _, condition := range step.Preconditions {
		if err := ValidateCondition(condition, PhasePre); err != nil {
			return withStep(err, index)
		}
	}
	for _, condition := range step.Postconditions {
		if err := ValidateCondition(condition, PhasePost); err != nil {
			return withStep(err, index)
		}
	}
	return nil
}

func withStep(err error, index int) error {
	var target *Violation
	if errors.As(err, &target) {
		clone := *target
		clone.Step = index
		clone.Message = fmt.Sprintf("case.steps[%d]: %s", index, target.Message)
		return &clone
	}
	return err
}

// ValidateCondition 按唯一的阶段表校验条件。
func ValidateCondition(condition Condition, phase Phase) error {
	set, known := conditionPhases[condition.Type]
	if !known {
		return violation(
			CodeUnknownConditionType, -1,
			"condition has unknown type %q; allowed types: %s",
			condition.Type, strings.Join(conditionTypeNames(), ", "),
		)
	}
	allowed := set.post
	if phase == PhasePre {
		allowed = set.pre
	}
	if !allowed {
		return violation(
			CodeConditionPhase, -1,
			"condition %q asserts a change or an event and cannot hold before the action; declare it as a postcondition or use %s",
			condition.Type, strings.Join(PreconditionTypeNames(), ", "),
		)
	}
	if condition.Value == "" {
		return violation(CodeConditionMissingValue, -1, "condition %q requires a value", condition.Type)
	}
	if condition.TimeoutMS <= 0 {
		return violation(CodeConditionMissingTimeout, -1, "condition %q requires a positive timeout_ms", condition.Type)
	}
	return nil
}

func validateTarget(index int, target Target) error {
	if strings.TrimSpace(target.Hint) == "" {
		return violation(CodeMissingTarget, index, "case.steps[%d].target.hint is required", index)
	}
	if err := ValidateLocator(target.Locator); err != nil {
		return withStep(err, index)
	}
	if target.Grounding.ObservationID == "" ||
		target.Grounding.CandidateID == "" ||
		target.Grounding.PageURL == "" {
		return violation(
			CodeTargetUngrounded, index,
			"case.steps[%d].target must be grounded against a real observation: observation_id, candidate_id and page_url are required",
			index,
		)
	}
	return nil
}

// ValidateLocator 校验定位器形状。
func ValidateLocator(locator Locator) error {
	switch locator.Kind {
	case "role":
		if locator.Role == "" || locator.Name == "" {
			return violation(CodeInvalidLocator, -1, "role locator requires role and name")
		}
	case "text":
		if locator.Text == "" {
			return violation(CodeInvalidLocator, -1, "text locator requires text")
		}
	case "css":
		if locator.CSS == "" {
			return violation(CodeInvalidLocator, -1, "css locator requires css")
		}
	default:
		return violation(
			CodeInvalidLocator, -1,
			"unknown locator kind %q; allowed kinds: role, text, css", locator.Kind,
		)
	}
	return nil
}

// ContentHash 是工件的寻址标识。
func (c Case) ContentHash() string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// JSON 返回确定性序列化结果（结构体无 map，字段序稳定）。
func (c Case) JSON() []byte {
	raw, err := json.Marshal(c)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func normalize(c *Case) {
	if c.CaseVersion == "" {
		c.CaseVersion = CaseVersion
	}
	for index := range c.Steps {
		step := &c.Steps[index]
		step.Index = index
		if step.TimeoutMS <= 0 {
			step.TimeoutMS = DefaultStepTimeoutMS
		}
		if step.Preconditions == nil {
			step.Preconditions = []Condition{}
		}
		if step.Postconditions == nil {
			step.Postconditions = []Condition{}
		}
		for conditionIndex := range step.Preconditions {
			fillConditionTimeout(&step.Preconditions[conditionIndex])
		}
		for conditionIndex := range step.Postconditions {
			fillConditionTimeout(&step.Postconditions[conditionIndex])
		}
	}
}

func fillConditionTimeout(condition *Condition) {
	if condition.TimeoutMS <= 0 {
		condition.TimeoutMS = DefaultConditionTimeoutMS
	}
}

func isAbsoluteURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != ""
}

// PageAnchor 把页面 URL 归约成用于 url_contains 的稳定锚点。
// 根路径用 host 代替，避免退化成永远成立的 "/"。
func PageAnchor(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	path := parsed.Path
	if path == "" || path == "/" {
		if parsed.Host != "" {
			return parsed.Host
		}
		return rawURL
	}
	return path
}
