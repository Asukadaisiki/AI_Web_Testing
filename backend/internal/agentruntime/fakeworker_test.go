package agentruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// fakeSite 是一个内存里的假站点，用来离线、确定性地验证整条闭环。
//
// 它实现的是执行器语义的一个最小子集：页面、元素、定位器命中、动作引起的跳转、
// 以及条件判定。真执行器（Python + Playwright）必须给出同样形状的结果。
type fakeSite struct {
	mu     sync.Mutex
	broken bool
}

// Break 模拟"站点在干跑之后变了"：列表页上的 Widget 链接消失。
func (s *fakeSite) Break() {
	s.mu.Lock()
	s.broken = true
	s.mu.Unlock()
}

func (s *fakeSite) isBroken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}

const (
	listURL   = "https://shop.test/products"
	detailURL = "https://shop.test/item/1"
	cartURL   = "https://shop.test/view_cart"
)

// searchGlyph 是 Font Awesome 搜索图标（U+F002）——纯图标按钮的"可访问名"。
//
// 假站点故意让它当搜索提交按钮：真实站点就是这样（`<i class="fa fa-search">` 没加
// `aria-hidden`），可访问名与 text 都是同一个不可见码位，模型不可能用任何 hint 指到它。
// 于是"在列表页搜索"只能靠 input 的 submit=true 走回车。
const searchGlyph = "\uf002"

func roleLocator(role, name string) contract.Locator {
	return contract.Locator{Kind: "role", Role: role, Name: name, Exact: true, MatchCount: 1}
}

func textLocator(text string) contract.Locator {
	return contract.Locator{Kind: "text", Text: text, Exact: true, MatchCount: 1}
}

func element(ref, tag, role, name, text string, value *string, locators ...contract.Locator) contract.Element {
	return contract.Element{
		Ref: ref, Tag: tag, Role: role, Name: name, Text: text, Value: value,
		Visible: true, Enabled: true, Locators: locators,
	}
}

func observation(url, title string, elements []contract.Element) contract.Observation {
	return contract.Observation{
		ObservationID:  "obs_" + fmt.Sprint(len(elements)) + "_" + url,
		PageStateID:    "ps_" + url,
		URL:            url,
		Title:          title,
		Elements:       elements,
		ScreenshotPath: "",
	}
}

// page 返回某个 URL 在假站点里的观测。values 是输入框的当前值。
func (s *fakeSite) page(url string, values map[string]string) (contract.Observation, bool) {
	switch {
	case strings.HasPrefix(url, listURL):
		elements := []contract.Element{
			element("e1", "input", "textbox", "Search", "", nil, roleLocator("textbox", "Search")),
			// 纯图标搜索按钮：模型指不到它（F1），只能靠 input 的 submit 走回车。
			element("e2", "button", "button", searchGlyph, searchGlyph, nil, roleLocator("button", searchGlyph)),
			element("e4", "button", "button", "Add to cart", "Add to cart", nil, roleLocator("button", "Add to cart")),
		}
		if !s.isBroken() {
			elements = append(elements,
				element("e3", "a", "link", "Widget", "Widget", nil, textLocator("Widget")))
		}
		return observation(url, "All Products", elements), true
	case strings.HasPrefix(url, detailURL):
		quantity := values["Quantity"]
		elements := []contract.Element{
			element("e5", "input", "spinbutton", "Quantity", "", &quantity, roleLocator("spinbutton", "Quantity")),
			element("e6", "button", "button", "Add to cart", "Add to cart", nil, roleLocator("button", "Add to cart")),
			element("e7", "h1", "heading", "Widget", "Widget", nil, roleLocator("heading", "Widget")),
		}
		return observation(url, "Widget", elements), true
	case strings.HasPrefix(url, cartURL):
		elements := []contract.Element{
			element("e8", "button", "button", "Proceed To Checkout", "Proceed To Checkout", nil,
				roleLocator("button", "Proceed To Checkout")),
			element("e9", "td", "cell", "Widget", "Widget", nil, textLocator("Widget")),
		}
		return observation(url, "Shopping Cart", elements), true
	default:
		return contract.Observation{}, false
	}
}

// clickTarget 是"点这个元素会去哪"。
func clickTarget(url, name string) string {
	switch name {
	case "Widget":
		return detailURL
	case "Add to cart":
		if strings.HasPrefix(url, detailURL) {
			return cartURL
		}
		return url
	case "Proceed To Checkout":
		return "https://shop.test/checkout"
	default:
		return url
	}
}

// matchLocator 在观测里按定位器找元素。
func matchLocator(elements []contract.Element, locator contract.Locator) (contract.Element, bool) {
	for _, item := range elements {
		for _, candidate := range item.Locators {
			if candidate == locator {
				return item, true
			}
		}
	}
	return contract.Element{}, false
}

// evalCondition 是条件判定的最小实现，语义与 CONTRACT §2.2 一致。
func evalCondition(
	condition contract.Condition, before, after contract.Observation, targetValue *string,
) (bool, string) {
	switch condition.Type {
	case contract.CondURLContains:
		if strings.Contains(after.URL, condition.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("url %q does not contain %q", after.URL, condition.Value)
	case contract.CondURLChanges:
		if before.URL != after.URL {
			return true, ""
		}
		return false, fmt.Sprintf("url did not change from %q", before.URL)
	case contract.CondTextVisible:
		if hasText(after, condition.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("no visible element contains %q", condition.Value)
	case contract.CondTextGone:
		if !hasText(after, condition.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("an element still contains %q", condition.Value)
	case contract.CondValueEquals:
		if targetValue != nil && *targetValue == condition.Value {
			return true, ""
		}
		actual := "<nil>"
		if targetValue != nil {
			actual = *targetValue
		}
		return false, fmt.Sprintf("target value is %q, want %q", actual, condition.Value)
	default:
		return false, fmt.Sprintf("unknown condition type %q", condition.Type)
	}
}

func hasText(observation contract.Observation, needle string) bool {
	needle = strings.ToLower(needle)
	for _, item := range observation.Elements {
		if !item.Visible {
			continue
		}
		if strings.Contains(strings.ToLower(item.Text), needle) ||
			strings.Contains(strings.ToLower(item.Name), needle) {
			return true
		}
	}
	return false
}

// fakeWorker 是假执行器的 HTTP 外壳。
type fakeWorker struct {
	site     *fakeSite
	server   *httptest.Server
	mu       sync.Mutex
	sessions map[string]map[string]string
	execErr  bool
	// artifactSessions 记录每一次"该往哪个会话写产物"的声明（开浏览器会话 + 执行）。
	artifactSessions []string
	// actRequests 记录作者态动作请求：用来断言工具参数确实透传到了执行器
	// （例如 input 的 submit 必须到达作者态，否则观测停在原页面）。
	actRequests []contract.ActRequest
}

func newFakeWorker(site *fakeSite) *fakeWorker {
	worker := &fakeWorker{site: site, sessions: map[string]map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeFake(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		// 领域会话必须传进来：观测截图按它落目录（CONTRACT §9.2）。
		var body contract.OpenSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if body.SessionID == "" {
			writeFakeError(w, http.StatusBadRequest, "session_required", "session_id is required")
			return
		}
		worker.mu.Lock()
		worker.artifactSessions = append(worker.artifactSessions, body.SessionID)
		worker.mu.Unlock()
		id := fmt.Sprintf("bsess_%d", time.Now().UnixNano())
		worker.mu.Lock()
		worker.sessions[id] = map[string]string{}
		worker.mu.Unlock()
		writeFake(w, http.StatusOK, contract.Session{SessionID: id})
	})
	mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		worker.mu.Lock()
		delete(worker.sessions, r.PathValue("id"))
		worker.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /sessions/{id}/navigate", func(w http.ResponseWriter, r *http.Request) {
		var body contract.NavigateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		values, ok := worker.values(r.PathValue("id"))
		if !ok {
			writeFakeError(w, http.StatusNotFound, "session_not_found", "no session")
			return
		}
		page, ok := worker.site.page(body.URL, values)
		if !ok {
			writeFakeError(w, http.StatusBadRequest, "worker_error", "unknown url "+body.URL)
			return
		}
		worker.setURL(r.PathValue("id"), body.URL)
		writeFake(w, http.StatusOK, page)
	})
	mux.HandleFunc("POST /sessions/{id}/act", func(w http.ResponseWriter, r *http.Request) {
		var body contract.ActRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		worker.mu.Lock()
		worker.actRequests = append(worker.actRequests, body)
		worker.mu.Unlock()
		values, ok := worker.values(r.PathValue("id"))
		if !ok {
			writeFakeError(w, http.StatusNotFound, "session_not_found", "no session")
			return
		}
		current := worker.url(r.PathValue("id"))
		page, ok := worker.site.page(current, values)
		if !ok {
			writeFakeError(w, http.StatusBadRequest, "worker_error", "unknown url "+current)
			return
		}
		target, found := matchLocator(page.Elements, body.Locator)
		if !found {
			writeFakeError(w, http.StatusBadRequest, "target_not_found", "locator matched nothing")
			return
		}
		if body.Action == contract.ActionInput {
			values[target.Name] = body.Value
			page, _ = worker.site.page(current, values)
			writeFake(w, http.StatusOK, page)
			return
		}
		next := clickTarget(current, target.Name)
		worker.setURL(r.PathValue("id"), next)
		page, _ = worker.site.page(next, values)
		writeFake(w, http.StatusOK, page)
	})
	mux.HandleFunc("POST /execute", func(w http.ResponseWriter, r *http.Request) {
		var body contract.ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		// 执行也必须带会话：每步截图要落进 <session_id>/ 目录。
		if body.SessionID == "" {
			writeFakeError(w, http.StatusBadRequest, "session_required", "session_id is required")
			return
		}
		worker.mu.Lock()
		worker.artifactSessions = append(worker.artifactSessions, body.SessionID)
		worker.mu.Unlock()
		writeFake(w, http.StatusOK, worker.execute(body.Case))
	})
	worker.server = httptest.NewServer(mux)
	return worker
}

// artifactSessionIDs 返回假执行器收到过的全部"产物归哪个会话"的声明，
// 按收到顺序（开浏览器会话、干跑、真实执行都会声明一次）。
func (w *fakeWorker) artifactSessionIDs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.artifactSessions))
	copy(out, w.artifactSessions)
	return out
}

// actRequestSnapshot 返回假执行器收到过的全部作者态动作请求，按收到顺序。
func (w *fakeWorker) actRequestSnapshot() []contract.ActRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]contract.ActRequest, len(w.actRequests))
	copy(out, w.actRequests)
	return out
}

func (w *fakeWorker) values(sessionID string) (map[string]string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	values, ok := w.sessions[sessionID]
	return values, ok
}

func (w *fakeWorker) url(sessionID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sessions[sessionID]["__url"]
}

func (w *fakeWorker) setURL(sessionID, url string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if values, ok := w.sessions[sessionID]; ok {
		values["__url"] = url
	}
}

// FailExecution 让 /execute 返回执行器故障，用于验证 worker_error 信号。
func (w *fakeWorker) FailExecution() {
	w.mu.Lock()
	w.execErr = true
	w.mu.Unlock()
}

// Close 关掉假执行器。
func (w *fakeWorker) Close() { w.server.Close() }

// URL 是假执行器地址。
func (w *fakeWorker) URL() string { return w.server.URL }

// execute 在全新上下文里跑一遍 case，形状与真执行器一致。
func (w *fakeWorker) execute(artifact contract.Case) contract.ExecutionResult {
	w.mu.Lock()
	execErr := w.execErr
	w.mu.Unlock()
	started := time.Now().UTC()
	result := contract.ExecutionResult{
		ExecutionID: fmt.Sprintf("exec_%d", time.Now().UnixNano()),
		Status:      contract.ExecutionPassed,
		StartedAt:   started,
	}
	if execErr {
		result.Status = contract.ExecutionError
		result.FinishedAt = time.Now().UTC()
		result.Error = &contract.StepError{
			Kind: contract.SignalWorkerError, Message: "browser launch failed",
		}
		return result
	}

	values := map[string]string{}
	current := "about:blank"
	steps := make([]contract.StepResult, 0, len(artifact.Steps))
	for _, step := range artifact.Steps {
		stepStarted := time.Now().UTC()
		before, _ := w.site.page(current, values)
		before.URL = current
		stepResult := contract.StepResult{
			Index: step.Index, Action: step.Action, Status: "passed",
			StartedAt: stepStarted, URLBefore: current,
			Evidence: contract.Evidence{Console: []contract.ConsoleEntry{}, Network: []contract.NetworkEntry{}},
		}
		var targetValue *string
		switch step.Action {
		case contract.ActionGoto:
			current = *step.Value
		case contract.ActionClick, contract.ActionInput:
			page, ok := w.site.page(current, values)
			if !ok {
				stepResult.Status = "failed"
				stepResult.Error = &contract.StepError{
					Kind: contract.SignalTargetNotFound, Message: "page not found",
				}
			} else if target, found := matchLocator(page.Elements, step.Target.Locator); !found {
				stepResult.Status = "failed"
				stepResult.Error = &contract.StepError{
					Kind: contract.SignalTargetNotFound,
					Message: fmt.Sprintf("locator %s matched 0 elements",
						step.Target.Locator.Describe()),
				}
			} else if step.Action == contract.ActionInput {
				values[target.Name] = *step.Value
				value := values[target.Name]
				targetValue = &value
			} else {
				current = clickTarget(current, target.Name)
			}
		}
		after, ok := w.site.page(current, values)
		if !ok {
			after = contract.Observation{URL: current, Elements: []contract.Element{}}
		}
		if targetValue == nil && step.Target != nil {
			if page, ok := w.site.page(current, values); ok {
				if target, found := matchLocator(page.Elements, step.Target.Locator); found {
					targetValue = target.Value
				}
			}
		}
		for _, condition := range step.Preconditions {
			satisfied, detail := evalCondition(condition, before, before, targetValue)
			item := contract.ConditionResult{
				Phase: contract.PhasePre, Type: condition.Type, Value: condition.Value,
				Satisfied: satisfied,
			}
			if !satisfied {
				item.Detail = &detail
				stepResult.Status = "failed"
			}
			stepResult.Conditions = append(stepResult.Conditions, item)
		}
		for _, condition := range step.Postconditions {
			satisfied, detail := evalCondition(condition, before, after, targetValue)
			item := contract.ConditionResult{
				Phase: contract.PhasePost, Type: condition.Type, Value: condition.Value,
				Satisfied: satisfied,
			}
			if !satisfied {
				item.Detail = &detail
				stepResult.Status = "failed"
			}
			stepResult.Conditions = append(stepResult.Conditions, item)
		}
		stepResult.URLAfter = current
		stepResult.DurationMS = time.Since(stepStarted).Milliseconds()
		steps = append(steps, stepResult)
		if stepResult.Status == "failed" {
			result.Status = contract.ExecutionFailed
			break
		}
	}
	result.Steps = steps
	result.FinalURL = current
	result.FinishedAt = time.Now().UTC()
	return result
}

func writeFake(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeFakeError(w http.ResponseWriter, status int, code, detail string) {
	writeFake(w, status, contract.WorkerError{Error: code, Detail: detail})
}
