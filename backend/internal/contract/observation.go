package contract

// Element 是观测到的页面元素。
//
// Locators 是按偏好排序、且已在页面上就地验证过的定位器（CONTRACT.md §3.1）。
// 只有 match_count == 1 的定位器才允许出现在这里，这保证"观测时能解析、
// 执行时能命中"，也是旧版 BUG-211 一类问题的根治手段。
type Element struct {
	Ref      string    `json:"ref"`
	Tag      string    `json:"tag"`
	Role     string    `json:"role"`
	Name     string    `json:"name"`
	Text     string    `json:"text"`
	Value    *string   `json:"value"`
	Visible  bool      `json:"visible"`
	Enabled  bool      `json:"enabled"`
	Locators []Locator `json:"locators"`
}

// Observation 是一次页面观测，也是接地的唯一依据。
type Observation struct {
	ObservationID string `json:"observation_id"`
	PageStateID   string `json:"page_state_id"`
	// BrowserSessionID 是执行器内部的浏览器上下文句柄，用完即弃，
	// 与会话（session，CONTRACT §9）无关，不得混用。
	BrowserSessionID string    `json:"browser_session_id"`
	URL              string    `json:"url"`
	Title            string    `json:"title"`
	Elements         []Element `json:"elements"`
	ScreenshotPath   string    `json:"screenshot_path"`
}

// Session 是作者态（规划阶段）的浏览器会话句柄。
//
// SessionID 是 browser session（`bsess_...`），不是会话（CONTRACT §9.3）。
type Session struct {
	SessionID string `json:"session_id"`
}

// OpenSessionRequest 开一个浏览器会话，并声明它属于哪个领域会话——
// 观测截图要落进那个会话的产物目录（CONTRACT §9.2）。
type OpenSessionRequest struct {
	SessionID string `json:"session_id"`
}

// ActRequest 是作者态执行单个动作的请求，用于让模型"走到下一页"。
type ActRequest struct {
	Action  Action  `json:"action"`
	Locator Locator `json:"locator"`
	Value   string  `json:"value"`
}

// NavigateRequest 是作者态导航请求。
type NavigateRequest struct {
	URL string `json:"url"`
}

// ExecuteRequest 是执行整个 case 的请求。
//
// SessionID 必填：产物必须能落到会话目录里（CONTRACT §9.2）。
type ExecuteRequest struct {
	SessionID string `json:"session_id"`
	Case      Case   `json:"case"`
}

// WorkerError 是执行器返回的结构化错误。
type WorkerError struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
}
