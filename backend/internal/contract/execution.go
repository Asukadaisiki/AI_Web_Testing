package contract

import "time"

// ExecutionStatus 是一次执行的整体状态。
type ExecutionStatus string

const (
	ExecutionPassed ExecutionStatus = "passed"
	ExecutionFailed ExecutionStatus = "failed"
	ExecutionError  ExecutionStatus = "error"
)

// SignalKind 是报告里的失败信号种类。
type SignalKind string

const (
	SignalTargetNotFound SignalKind = "target_not_found"
	SignalConditionUnmet SignalKind = "condition_unmet"
	SignalStepTimeout    SignalKind = "step_timeout"
	SignalWorkerError    SignalKind = "worker_error"
	SignalCaseInvalid    SignalKind = "case_invalid"
)

// Message 返回信号的中文说明，供失败回灌候选复用。
func (k SignalKind) Message() string {
	switch k {
	case SignalTargetNotFound:
		return "元素定位在超时内未命中"
	case SignalConditionUnmet:
		return "步骤条件未满足"
	case SignalStepTimeout:
		return "步骤执行超时"
	case SignalWorkerError:
		return "执行器故障或不可达"
	case SignalCaseInvalid:
		return "用例未通过契约校验"
	default:
		return string(k)
	}
}

// ConditionResult 是单个条件的判定结果。
type ConditionResult struct {
	Phase     Phase         `json:"phase"`
	Type      ConditionType `json:"type"`
	Value     string        `json:"value"`
	Satisfied bool          `json:"satisfied"`
	Detail    *string       `json:"detail"`
}

// ConsoleEntry 是一条浏览器控制台记录。
type ConsoleEntry struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// NetworkEntry 是一条网络请求记录。
type NetworkEntry struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Status int    `json:"status"`
}

// Evidence 是单步的证据。
type Evidence struct {
	ScreenshotPath string         `json:"screenshot_path"`
	Console        []ConsoleEntry `json:"console"`
	Network        []NetworkEntry `json:"network"`
}

// StepError 是单步失败的结构化原因。
type StepError struct {
	Kind    SignalKind `json:"kind"`
	Message string     `json:"message"`
}

// StepResult 是单步执行结果。
type StepResult struct {
	Index      int               `json:"index"`
	Action     Action            `json:"action"`
	Status     string            `json:"status"` // passed | failed
	StartedAt  time.Time         `json:"started_at"`
	DurationMS int64             `json:"duration_ms"`
	URLBefore  string            `json:"url_before"`
	URLAfter   string            `json:"url_after"`
	Conditions []ConditionResult `json:"conditions"`
	Evidence   Evidence          `json:"evidence"`
	Error      *StepError        `json:"error"`
}

// ExecutionResult 是一次完整执行的结果。
type ExecutionResult struct {
	ExecutionID string          `json:"execution_id"`
	Status      ExecutionStatus `json:"status"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	FinalURL    string          `json:"final_url"`
	Steps       []StepResult    `json:"steps"`
	Error       *StepError      `json:"error"`
}
