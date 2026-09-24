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
	SignalTargetNotFound        SignalKind = "target_not_found"
	SignalConditionUnmet        SignalKind = "condition_unmet"
	SignalStepTimeout           SignalKind = "step_timeout"
	SignalWorkerError           SignalKind = "worker_error"
	SignalCaseInvalid           SignalKind = "case_invalid"
	SignalBlockedByDialog       SignalKind = "blocked_by_dialog"
	SignalBlockedByOverlay      SignalKind = "blocked_by_overlay"
	SignalBlockedByInterstitial SignalKind = "blocked_by_interstitial"
	SignalBlockedByCookieBanner SignalKind = "blocked_by_cookie_banner"
	SignalBlockedByAuth         SignalKind = "blocked_by_auth"
	SignalBlockedByCaptcha      SignalKind = "blocked_by_captcha"
	SignalBlockedByLoading      SignalKind = "blocked_by_loading"
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
	case SignalBlockedByDialog:
		return "目标被弹窗阻塞"
	case SignalBlockedByOverlay:
		return "目标被遮罩或固定层阻塞"
	case SignalBlockedByInterstitial:
		return "目标被插屏或广告阻塞"
	case SignalBlockedByCookieBanner:
		return "目标被 Cookie 横幅阻塞"
	case SignalBlockedByAuth:
		return "目标被登录墙阻塞"
	case SignalBlockedByCaptcha:
		return "目标被验证码阻塞"
	case SignalBlockedByLoading:
		return "目标被加载状态阻塞"
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

// HitTest 是动作前目标可达性检查的结果。
type HitTest struct {
	TargetRef   string  `json:"target_ref,omitempty"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	HitRef      string  `json:"hit_ref,omitempty"`
	HitTag      string  `json:"hit_tag,omitempty"`
	HitRole     string  `json:"hit_role,omitempty"`
	HitText     string  `json:"hit_text,omitempty"`
	Covered     bool    `json:"covered"`
	BlockerKind string  `json:"blocker_kind,omitempty"`
}

// RecoveryAttempt 记录执行器为移除通用 blocker 做过的一次安全恢复。
type RecoveryAttempt struct {
	Blocker               Blocker `json:"blocker"`
	Action                string  `json:"action"`
	Succeeded             bool    `json:"succeeded"`
	Reason                string  `json:"reason,omitempty"`
	BeforeScreenshotPath  string  `json:"before_screenshot_path,omitempty"`
	AfterScreenshotPath   string  `json:"after_screenshot_path,omitempty"`
	URLBefore             string  `json:"url_before,omitempty"`
	URLAfter              string  `json:"url_after,omitempty"`
	RetriedOriginalAction bool    `json:"retried_original_action"`
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
	Blocker    *Blocker          `json:"blocker,omitempty"`
	HitTest    *HitTest          `json:"hit_test,omitempty"`
	Recovery   []RecoveryAttempt `json:"recovery,omitempty"`
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
