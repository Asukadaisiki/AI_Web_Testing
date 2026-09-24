package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

// 一条真实感的离线脚本：打开列表 → 点 Widget → 断言详情页 → 结束。
// 模型只说工具与参数，一行 JSON 都不写。
func happyScript() []ScriptedStep {
	return []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"打开第一个商品的详情","expect_url":"/item/1"}`)},
		{Tool: "assert_text", Arguments: json.RawMessage(
			`{"text":"Add to cart","intent":"详情页出现加购按钮"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"打开商品详情"}`)},
	}
}

type harness struct {
	runtime *Runtime
	store   *store.Store
	site    *fakeSite
	fake    *fakeWorker
}

func newHarness(t *testing.T, steps []ScriptedStep) *harness {
	t.Helper()
	return newHarnessWithLLM(t, NewScriptedLLM(steps), 0)
}

// newHarnessWithLLM 允许换模型（例如报用量的假模型）并设定 token 预算。
func newHarnessWithLLM(t *testing.T, llm LLM, maxTokens int) *harness {
	t.Helper()
	site := &fakeSite{}
	fake := newFakeWorker(site)
	t.Cleanup(fake.Close)

	database, err := store.Open(filepath.Join(t.TempDir(), "loop.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	runtime := New(Config{
		Store:          database,
		Worker:         worker.New(fake.URL()),
		LLM:            llm,
		MaxModelCalls:  20,
		MaxTotalTokens: maxTokens,
		AnswerTimeout:  5 * time.Second,
	})
	return &harness{runtime: runtime, store: database, site: site, fake: fake}
}

// plan 建一个会话（含第 1 轮）并跑到 case 就绪。
func (h *harness) plan(t *testing.T, input string) store.Run {
	t.Helper()
	_, run, err := h.store.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("plan: %v", err)
	}
	reloaded, err := h.store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	return reloaded
}

// TestInputSubmitReachesTheAuthoringBrowser 钉住 submit 的透传链路：
// 模型调 input(submit=true) → 落库的步骤带 submit → 作者态执行请求也带 submit。
//
// 最后这一环很关键：作者态如果不真的提交，观测会停在原页面，
// 后续步骤的接地就锚在错的页面上（CONTRACT §2.1）。
func TestInputSubmitReachesTheAuthoringBrowser(t *testing.T) {
	h := newHarness(t, []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "input", Arguments: json.RawMessage(
			`{"hint":"Search","value":"widget","intent":"搜索并回车提交","expect_value":"widget","submit":true}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"回车提交搜索"}`)},
	})

	run := h.plan(t, "在列表页搜索 widget")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q", run.Status, store.StatusAwaitingApproval)
	}

	record, err := h.store.GetCase(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("persisted case must validate: %v", err)
	}
	if len(artifact.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(artifact.Steps))
	}
	inputStep := artifact.Steps[1]
	if inputStep.Action != contract.ActionInput {
		t.Fatalf("step 1 action = %q, want input", inputStep.Action)
	}
	if !inputStep.Submit {
		t.Fatal("the stored input step must carry submit")
	}

	// 作者态必须收到 submit=true；click 步骤则必须不带。
	var sawSubmit bool
	for _, request := range h.fake.actRequestSnapshot() {
		if request.Action != contract.ActionInput {
			if request.Submit {
				t.Fatalf("non-input act request carried submit: %+v", request)
			}
			continue
		}
		if request.Submit {
			sawSubmit = true
		}
	}
	if !sawSubmit {
		t.Fatal("the authoring browser never received submit=true")
	}
}

func TestCandidateIDClickReachesIconOnlyFormSubmit(t *testing.T) {
	h := newHarness(t, []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "input", Arguments: json.RawMessage(
			`{"hint":"Search","value":"widget","intent":"填写搜索词","expect_value":"widget"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"candidate_id":"act_search_submit","intent":"提交搜索表单","expect_url":"search=widget"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"候选提交搜索"}`)},
	})

	run := h.plan(t, "用站内搜索找 widget")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q (error: %v)", run.Status, store.StatusAwaitingApproval, run.Error)
	}

	record, err := h.store.GetCase(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("persisted case must validate: %v", err)
	}
	if len(artifact.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(artifact.Steps))
	}
	clickStep := artifact.Steps[2]
	if clickStep.Action != contract.ActionClick {
		t.Fatalf("step 2 action = %q, want click", clickStep.Action)
	}
	if clickStep.Target == nil || clickStep.Target.Grounding.CandidateID != "act_search_submit" {
		t.Fatalf("candidate grounding missing: %#v", clickStep.Target)
	}
	if clickStep.Target.Locator != cssLocator("#submit_search") {
		t.Fatalf("candidate locator = %#v", clickStep.Target.Locator)
	}
}

func TestStructuredTargetScopesDuplicateCardLinks(t *testing.T) {
	h := newHarness(t, []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `?cards=1","intent":"打开商品列表"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"View Product","target":{"object":{"role":"link","text":"View Product"},"scope":{"kind":"card","contains_text":"Blue Top"},"relation":"within"},"intent":"打开 Blue Top 的详情","expect_url":"/item/1"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"打开 Blue Top 详情"}`)},
	})

	run := h.plan(t, "打开 Blue Top 的商品详情")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q", run.Status, store.StatusAwaitingApproval)
	}
	record, err := h.store.GetCase(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("persisted case must validate: %v", err)
	}
	target := artifact.Steps[1].Target
	if target == nil || target.Spec == nil || target.Spec.Scope == nil {
		t.Fatalf("structured target was not preserved: %#v", target)
	}
	if target.Grounding.CandidateID == "" || target.Locator.Kind != "css" {
		t.Fatalf("target was not grounded to the scoped card link: %#v", target)
	}
}

// TestClosedLoopOffline 是整条闭环的离线验证：
// 输入 → 规划（工具调用构建 + 干跑验证）→ 审批 → 执行 → 报告。
func TestClosedLoopOffline(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, happyScript())

	run := h.plan(t, "打开第一个商品的详情页，并确认可以加入购物车")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q", run.Status, store.StatusAwaitingApproval)
	}
	if run.SessionID == nil || *run.SessionID == "" {
		t.Fatal("a planned run must belong to a session")
	}

	// 干跑也要声明产物归属：干跑截图同样是这个会话的证据（CONTRACT §9.2）。
	dryRunSessions := h.fake.artifactSessionIDs()
	if len(dryRunSessions) == 0 {
		t.Fatal("the dry run must tell the executor which session owns its evidence")
	}
	for _, id := range dryRunSessions {
		if id != *run.SessionID {
			t.Fatalf("dry run artifact session = %q, want %q", id, *run.SessionID)
		}
	}

	record, err := h.store.GetCase(ctx, run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	// 落库的 case 必须能直接过契约校验，且步骤是模型调用换来的。
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("persisted case must validate: %v", err)
	}
	if len(artifact.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(artifact.Steps))
	}
	if artifact.Steps[0].Action != contract.ActionGoto {
		t.Fatalf("first action = %q", artifact.Steps[0].Action)
	}
	if artifact.Steps[1].Action != contract.ActionClick || artifact.Steps[1].Target == nil {
		t.Fatalf("second step = %#v", artifact.Steps[1])
	}
	// 接地证据必须来自真实观测。
	grounding := artifact.Steps[1].Target.Grounding
	if grounding.ObservationID == "" || grounding.CandidateID == "" || grounding.PageURL == "" {
		t.Fatalf("target is not grounded: %#v", grounding)
	}
	if artifact.Steps[1].Target.Locator.MatchCount != 1 {
		t.Fatalf("locator must be verified with match_count 1: %#v", artifact.Steps[1].Target.Locator)
	}
	// 前置条件由 Go 派生，且是状态事实。
	for index, step := range artifact.Steps {
		if index == 0 {
			if len(step.Preconditions) != 0 {
				t.Fatalf("goto must not have preconditions: %#v", step.Preconditions)
			}
			continue
		}
		if len(step.Preconditions) != 1 || step.Preconditions[0].Type != contract.CondURLContains {
			t.Fatalf("step %d preconditions = %#v", index, step.Preconditions)
		}
	}

	if err := h.store.ApproveCase(ctx, record.ID, "owner"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := h.runtime.Execute(ctx, run); err != nil {
		t.Fatalf("execute: %v", err)
	}

	final, err := h.store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != store.StatusCompleted {
		t.Fatalf("final status = %q, want %q (error: %v)", final.Status, store.StatusCompleted, final.Error)
	}

	// 规划（含干跑）与真实执行声明的产物会话，必须全是这一轮所属的会话。
	allSessions := h.fake.artifactSessionIDs()
	if len(allSessions) < 2 {
		t.Fatalf("expected the dry run and the real execution to declare a session, got %v", allSessions)
	}
	for _, id := range allSessions {
		if id != *run.SessionID {
			t.Fatalf("artifact session = %q, want %q", id, *run.SessionID)
		}
	}

	execution, err := h.store.GetExecution(ctx, run.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	var result contract.ExecutionResult
	if err := json.Unmarshal(execution.Result, &result); err != nil {
		t.Fatalf("decode execution: %v", err)
	}
	if result.Status != contract.ExecutionPassed {
		t.Fatalf("execution status = %q", result.Status)
	}

	signals, err := h.store.ListSignals(ctx, run.ID)
	if err != nil {
		t.Fatalf("list signals: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("a passing run must have no signals: %#v", signals)
	}
	candidates, err := h.store.ListFeedback(ctx, run.ID)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("a passing run must not propose feedback: %#v", candidates)
	}

	// 事件流必须能让前端把时间线画出来。
	events, err := h.store.ListEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	types := map[string]int{}
	for _, event := range events {
		types[event.Type]++
	}
	for _, want := range []string{EventRunStatus, EventToolCall, EventObservation, EventCaseReady, EventExecutionDone, EventReportReady} {
		if types[want] == 0 {
			t.Fatalf("missing event type %q in %v", want, types)
		}
	}
	if types[EventReportReady] != 1 {
		t.Fatalf("report_ready = %d, want 1", types[EventReportReady])
	}
}

// TestFailureIsFedBackAsCandidates 验证"报告 → 失败回灌"：
// 干跑通过之后站点变了，真实执行失败，信号与候选必须自动产出。
func TestFailureIsFedBackAsCandidates(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, happyScript())

	run := h.plan(t, "打开第一个商品的详情页")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q", run.Status)
	}
	// 站点在干跑之后变了：列表页上的 Widget 链接消失。
	h.site.Break()

	record, err := h.store.GetCase(ctx, run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	if err := h.store.ApproveCase(ctx, record.ID, "owner"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := h.runtime.Execute(ctx, run); err != nil {
		t.Fatalf("execute: %v", err)
	}

	final, err := h.store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	// 用例失败但闭环走完了：状态是 completed，失败体现在报告与信号里。
	if final.Status != store.StatusCompleted {
		t.Fatalf("status = %q, want %q", final.Status, store.StatusCompleted)
	}

	signals, err := h.store.ListSignals(ctx, run.ID)
	if err != nil {
		t.Fatalf("list signals: %v", err)
	}
	if len(signals) == 0 {
		t.Fatal("a failing run must produce signals")
	}
	kinds := map[string]bool{}
	for _, signal := range signals {
		kinds[signal.Kind] = true
		if signal.Message == "" {
			t.Fatalf("signal must carry a message: %#v", signal)
		}
	}
	if !kinds[string(contract.SignalTargetNotFound)] {
		t.Fatalf("expected target_not_found, got %v", kinds)
	}

	candidates, err := h.store.ListFeedback(ctx, run.ID)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("a failing run must propose feedback candidates")
	}
	if len(candidates) > 3 {
		t.Fatalf("candidates = %d, want <= 3", len(candidates))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate.SignalKind] {
			t.Fatalf("candidate kinds must be deduped: %#v", candidates)
		}
		seen[candidate.SignalKind] = true
		if candidate.Status != "pending" {
			t.Fatalf("candidate must start pending: %#v", candidate)
		}
		// 候选输入 = 原输入 + 结构化失败摘要，人可以在其基础上改。
		if !strings.Contains(candidate.ProposedInput, "打开第一个商品的详情页") {
			t.Fatalf("candidate must keep the original input: %q", candidate.ProposedInput)
		}
		if !strings.Contains(candidate.ProposedInput, candidate.SignalKind) {
			t.Fatalf("candidate must mention the failure kind: %q", candidate.ProposedInput)
		}
	}
}

// TestUngroundedHintIsRejectedAndModelRetries 验证"接地"这道闸：
// 模型第一次说了一个页面上不存在的目标，工具必须拒绝并给出候选，模型改口后成功。
func TestUngroundedHintIsRejectedAndModelRetries(t *testing.T) {
	ctx := context.Background()
	steps := []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Buy now","intent":"点一个不存在的按钮","expect_text":"x"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"改口点真实存在的链接","expect_url":"/item/1"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"改口后的用例"}`)},
	}
	h := newHarness(t, steps)

	run := h.plan(t, "打开第一个商品")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q (error: %v)", run.Status, run.Error)
	}
	record, err := h.store.GetCase(ctx, run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	// 被拒绝的那次调用不留下任何步骤。
	if len(artifact.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(artifact.Steps))
	}
	if artifact.Steps[1].Target == nil || artifact.Steps[1].Target.Hint != "Widget" {
		t.Fatalf("second step target = %#v", artifact.Steps[1].Target)
	}

	// 模型确实在当前状态里收到了错误码与可重新选择的页面元素。
	scripted := h.runtime.llm.(*ScriptedLLM)
	found := false
	for _, messages := range scripted.CallsSeen() {
		if len(messages) != 4 {
			continue
		}
		var state struct {
			Page *struct {
				Elements []json.RawMessage `json:"elements"`
			} `json:"current_page"`
			LastResult *struct {
				Error string `json:"error"`
			} `json:"last_result"`
		}
		decodeContextMessage(t, messages[3].Content, currentStatePrefix, &state)
		if state.LastResult != nil && state.LastResult.Error == "target_not_found" {
			found = true
			if state.Page == nil || len(state.Page.Elements) == 0 {
				t.Fatal("a rejected target must retain the current page choices")
			}
		}
	}
	if !found {
		t.Fatal("the model never saw target_not_found in canonical state")
	}
}

// TestFinishRefusesACaseThatFailsItsDryRun 验证"生成的就是错的"在审批前就被拦住，
// 并且模型能按结构化反馈改口修好（drop_last_step 重建尾巴 + open_page 重新接地）。
func TestFinishRefusesACaseThatFailsItsDryRun(t *testing.T) {
	ctx := context.Background()
	steps := []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "assert_text", Arguments: json.RawMessage(
			`{"text":"This never appears","intent":"制造一个只能在干跑发现的错误"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"第一次注定失败"}`)},
		{Tool: "drop_last_step", Arguments: json.RawMessage(`{}`)},
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"重新接地"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"用正确的期望重做","expect_url":"/item/1"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"修好之后的用例"}`)},
	}
	h := newHarness(t, steps)

	run := h.plan(t, "打开第一个商品")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q (error: %v)", run.Status, run.Error)
	}

	// 模型必须看到干跑失败的结构化反馈。
	scripted := h.runtime.llm.(*ScriptedLLM)
	sawFailure := false
	for _, message := range scripted.Seen() {
		if strings.Contains(message.Content, "dry_run_failed") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("the model never saw dry_run_failed in canonical state")
	}

	// 事件流里也必须留下失败明细。只写一句 "did not pass a full dry run"
	// 会让读事件的人（和事后复盘）完全看不到是哪一步、哪个条件没过——
	// 模型在对话里看得到，运维侧看不到，等于这个失败在事件流里是隐形的。
	events, err := h.store.ListEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	sawDetail := false
	for _, event := range events {
		if event.Type != EventToolCall {
			continue
		}
		var payload struct {
			Tool    string `json:"tool"`
			Error   string `json:"error"`
			Failure *struct {
				Status      string   `json:"status"`
				CurrentURL  string   `json:"current_url"`
				RepairHints []string `json:"repair_hints"`
				FailedStep  *struct {
					Index int `json:"index"`
				} `json:"failed_step"`
				Steps []struct {
					Index       int      `json:"index"`
					Action      string   `json:"action"`
					Status      string   `json:"status"`
					Error       string   `json:"error"`
					Unsatisfied []string `json:"unsatisfied_conditions"`
				} `json:"steps"`
			} `json:"failure"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}
		if payload.Tool != "finish_case" || payload.Error != "dry_run_failed" {
			continue
		}
		if payload.Failure == nil || len(payload.Failure.Steps) == 0 {
			t.Fatalf("dry_run_failed event must carry the failing steps: %s", event.Payload)
		}
		if payload.Failure.FailedStep == nil || payload.Failure.CurrentURL == "" || len(payload.Failure.RepairHints) == 0 {
			t.Fatalf("dry_run_failed event must carry repair context: %s", event.Payload)
		}
		if payload.Failure.Steps[0].Status == "passed" {
			t.Fatalf("dry-run failure must list only non-passed steps: %s", event.Payload)
		}
		sawDetail = true
	}
	if !sawDetail {
		t.Fatal("no dry_run_failed tool_call event with step detail found")
	}

	// 落库的是修好之后的那一份：期望已被改对。
	record, err := h.store.GetCase(ctx, run.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	artifact, err := contract.Validate(record.Payload)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	last := artifact.Steps[len(artifact.Steps)-1]
	if last.Action != contract.ActionClick || last.Postconditions[0].Value != "/item/1" {
		t.Fatalf("the stored case was not the corrected one: %#v", last)
	}
}

// TestDryRunThatNeverPassesFailsTheRun 验证一直不过的用例不会被落库、也不会进入审批。
func TestDryRunThatNeverPassesFailsTheRun(t *testing.T) {
	ctx := context.Background()
	steps := []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "assert_text", Arguments: json.RawMessage(
			`{"text":"This never appears","intent":"制造一个只能在干跑发现的错误"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"注定失败"}`)},
		{Final: "我无法让它通过。"},
	}
	h := newHarness(t, steps)

	_, run, err := h.store.CreateSession(ctx, "打开第一个商品")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := h.runtime.Plan(ctx, run); err == nil {
		t.Fatal("plan must fail when the model stops calling tools without a verified case")
	}
	final, err := h.store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != store.StatusFailed {
		t.Fatalf("status = %q, want %q", final.Status, store.StatusFailed)
	}
	if _, err := h.store.GetCase(ctx, run.ID); err == nil {
		t.Fatal("a case that fails its dry run must not be stored")
	}
}

// TestAskUserPausesTheRun 验证 ask_user 会把 run 挂起等人回答。
func TestAskUserPausesTheRun(t *testing.T) {
	h := newHarness(t, []ScriptedStep{
		{Ask: "要测哪个商品？"},
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开列表"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"回答后的用例"}`)},
	})

	ctx := context.Background()
	_, run, err := h.store.CreateSession(ctx, "测一个商品")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- h.runtime.Plan(ctx, run) }()

	// 等到 run 进入 awaiting_input。
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := h.store.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if current.Status == store.StatusAwaitingInput {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never reached awaiting_input (status %q)", current.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !h.runtime.Answer(run.ID, "就测 Widget") {
		t.Fatal("Answer must be accepted while awaiting input")
	}
	if err := <-done; err != nil {
		t.Fatalf("plan: %v", err)
	}

	final, err := h.store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q (error: %v)", final.Status, final.Error)
	}
	// 回答必须真的传给了模型。
	scripted := h.runtime.llm.(*ScriptedLLM)
	found := false
	for _, message := range scripted.Seen() {
		if strings.Contains(message.Content, "就测 Widget") {
			found = true
		}
	}
	if !found {
		t.Fatal("the answer never reached the model")
	}
}

// TestScriptedModelServesEveryRun 验证脚本模型不是一次性的：
// 同一个 Runtime 连开两个 run，第二个也必须能从头跑完（离线跑第二遍的常见用法）。
func TestScriptedModelServesEveryRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, happyScript())

	for attempt := 1; attempt <= 2; attempt++ {
		run := h.plan(t, "打开第一个商品的详情页")
		if run.Status != store.StatusAwaitingApproval {
			t.Fatalf("attempt %d: status = %q (error: %v)", attempt, run.Status, run.Error)
		}
		if _, err := h.store.GetCase(ctx, run.ID); err != nil {
			t.Fatalf("attempt %d: get case: %v", attempt, err)
		}
	}
}

// TestUnreachableWorkerFailsTheRun 验证执行器不可达时 run 明确失败，而不是静默卡住。
func TestUnreachableWorkerFailsTheRun(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(filepath.Join(t.TempDir(), "loop.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()
	runtime := New(Config{
		Store:         database,
		Worker:        worker.New("http://127.0.0.1:1"),
		LLM:           NewScriptedLLM(happyScript()),
		MaxModelCalls: 5,
		AnswerTimeout: time.Second,
	})
	_, run, err := database.CreateSession(ctx, "随便测点什么")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := runtime.Plan(ctx, run); err == nil {
		t.Fatal("plan must fail when the executor is unreachable")
	}
	final, err := database.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if final.Status != store.StatusFailed {
		t.Fatalf("status = %q, want %q", final.Status, store.StatusFailed)
	}
	if final.Error == nil || *final.Error == "" {
		t.Fatal("a failed run must record why")
	}
}
