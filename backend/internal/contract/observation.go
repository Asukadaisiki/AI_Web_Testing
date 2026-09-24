package contract

// Element 是观测到的页面元素。
//
// Locators 是按偏好排序、且已在页面上就地验证过的定位器（CONTRACT.md §3.1）。
// 只有 match_count == 1 的定位器才允许出现在这里，这保证"观测时能解析、
// 执行时能命中"，也是旧版 BUG-211 一类问题的根治手段。
type Element struct {
	Ref               string            `json:"ref"`
	Tag               string            `json:"tag"`
	Role              string            `json:"role"`
	Name              string            `json:"name"`
	Text              string            `json:"text"`
	Value             *string           `json:"value"`
	Visible           bool              `json:"visible"`
	Enabled           bool              `json:"enabled"`
	Locators          []Locator         `json:"locators"`
	ParentRef         string            `json:"parent_ref,omitempty"`
	ContainerRef      string            `json:"container_ref,omitempty"`
	OwnText           string            `json:"own_text,omitempty"`
	FullText          string            `json:"full_text,omitempty"`
	BBox              BoundingBox       `json:"bbox,omitempty"`
	VisibleInViewport bool              `json:"visible_in_viewport,omitempty"`
	ZIndex            *int              `json:"z_index,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	Form              *ElementFormInfo  `json:"form,omitempty"`
}

// BoundingBox 是元素或结构节点在 viewport 坐标系里的几何事实。
type BoundingBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ElementFormInfo 描述一个控件所属表单与提交候选。
type ElementFormInfo struct {
	FormRef            string `json:"form_ref"`
	SubmitCandidateRef string `json:"submit_candidate_ref,omitempty"`
	EnterSubmittable   bool   `json:"enter_submittable"`
}

// StructureNode 是可作为 TargetSpec scope 的页面结构节点。
type StructureNode struct {
	Ref                string            `json:"ref"`
	Kind               string            `json:"kind"`
	Tag                string            `json:"tag"`
	Role               string            `json:"role,omitempty"`
	ParentRef          string            `json:"parent_ref,omitempty"`
	FullText           string            `json:"full_text"`
	Visible            bool              `json:"visible"`
	BBox               BoundingBox       `json:"bbox,omitempty"`
	Attributes         map[string]string `json:"attributes,omitempty"`
	SubmitCandidateRef string            `json:"submit_candidate_ref,omitempty"`
	EnterSubmittable   bool              `json:"enter_submittable,omitempty"`
}

// Blocker 是页面上通用阻塞物的观测摘要，例如弹层、cookie banner、登录墙或验证码。
type Blocker struct {
	Kind              string    `json:"kind"`
	Ref               string    `json:"ref,omitempty"`
	Confidence        string    `json:"confidence"`
	CoversTargetRef   string    `json:"covers_target_ref,omitempty"`
	DismissCandidates []Locator `json:"dismiss_candidates,omitempty"`
	Reason            string    `json:"reason"`
}

// CandidateRelation records why an action candidate is relevant to nearby page structure.
type CandidateRelation struct {
	Type  string `json:"type"`
	Ref   string `json:"ref"`
	Label string `json:"label,omitempty"`
}

// ActionCandidate is a system-verified target option from the latest observation.
// Models may select it by candidate_id; the planner still validates action
// compatibility and uses only the verified locator stored here.
type ActionCandidate struct {
	CandidateID string              `json:"candidate_id"`
	Kind        string              `json:"kind"`
	Action      string              `json:"action"`
	TargetRef   string              `json:"target_ref"`
	Role        string              `json:"role,omitempty"`
	Name        string              `json:"name,omitempty"`
	Text        string              `json:"text,omitempty"`
	Aliases     []string            `json:"aliases,omitempty"`
	Attributes  map[string]string   `json:"attributes,omitempty"`
	Relations   []CandidateRelation `json:"relations,omitempty"`
	Locator     Locator             `json:"locator"`
	Confidence  string              `json:"confidence"`
}

// TargetSpec 是 v2 结构化定位请求。模型提供 object/scope 语义；系统仍只
// 输出已在 Observation 中验证过的 locator。
type TargetSpec struct {
	Object   TargetObject `json:"object"`
	Scope    *TargetScope `json:"scope,omitempty"`
	Relation string       `json:"relation,omitempty"`
	Role     string       `json:"role,omitempty"`
	Text     string       `json:"text,omitempty"`
	Name     string       `json:"name,omitempty"`
	Aliases  []string     `json:"aliases,omitempty"`
}

type TargetObject struct {
	Role    string   `json:"role,omitempty"`
	Text    string   `json:"text,omitempty"`
	Name    string   `json:"name,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

type TargetScope struct {
	Kind         string `json:"kind,omitempty"`
	ContainsText string `json:"contains_text,omitempty"`
	Ref          string `json:"ref,omitempty"`
}

// Observation 是一次页面观测，也是接地的唯一依据。
type Observation struct {
	ObservationID string `json:"observation_id"`
	PageStateID   string `json:"page_state_id"`
	// BrowserSessionID 是执行器内部的浏览器上下文句柄，用完即弃，
	// 与会话（session，CONTRACT §9）无关，不得混用。
	BrowserSessionID string            `json:"browser_session_id"`
	URL              string            `json:"url"`
	Title            string            `json:"title"`
	Elements         []Element         `json:"elements"`
	Structures       []StructureNode   `json:"structures,omitempty"`
	Blockers         []Blocker         `json:"blockers,omitempty"`
	ActionCandidates []ActionCandidate `json:"action_candidates,omitempty"`
	Truncated        bool              `json:"truncated,omitempty"`
	TruncationReason string            `json:"truncation_reason,omitempty"`
	ScreenshotPath   string            `json:"screenshot_path"`
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
//
// Submit 只对 input 有意义：填完之后按回车提交。作者态必须真的提交，
// 否则观测停在原页面，后续步骤的接地就会锚在错的页面上。
type ActRequest struct {
	Action         Action      `json:"action"`
	Locator        Locator     `json:"locator"`
	Value          string      `json:"value"`
	Submit         bool        `json:"submit,omitempty"`
	Postconditions []Condition `json:"postconditions"`
}

// ActResponse 是作者态动作的事务结果：动作后的期望先判定，再生成新观测。
type ActResponse struct {
	Status      string            `json:"status"`
	Observation Observation       `json:"observation"`
	Conditions  []ConditionResult `json:"conditions"`
	Error       *StepError        `json:"error,omitempty"`
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
