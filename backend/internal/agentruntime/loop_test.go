package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/worker"
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

func (h *harness) plan(t *testing.T, input string) store.Run {
	t.Helper()
	run, err := h.store.CreateRun(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
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

// TestClosedLoopOffline 是整条闭环的离线验证：
// 输入 → 规划（工具调用构建 + 干跑验证）→ 审批 → 执行 → 报告。
func TestClosedLoopOffline(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, happyScript())

	run := h.plan(t, "打开第一个商品的详情页，并确认可以加入购物车")
	if run.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want %q", run.Status, store.StatusAwaitingApproval)
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

	// 模型确实收到了带错误码的拒绝与候选。
	scripted := h.runtime.llm.(*ScriptedLLM)
	found := false
	for _, message := range scripted.Seen() {
		if message.Role != RoleTool {
			continue
		}
		var result struct {
			OK         bool `json:"ok"`
			Error      string
			Candidates []struct {
				Name string
			}
		}
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			continue
		}
		if result.Error == "target_not_found" {
			found = true
			if len(result.Candidates) == 0 {
				t.Fatal("a rejected target must come with candidates")
			}
		}
	}
	if !found {
		t.Fatal("the model never saw a target_not_found tool result")
	}
}

// TestFinishRefusesACaseThatFailsItsDryRun 验证"生成的就是错的"在审批前就被拦住，
// 并且模型能按结构化反馈改口修好（drop_last_step 重建尾巴 + open_page 重新接地）。
func TestFinishRefusesACaseThatFailsItsDryRun(t *testing.T) {
	ctx := context.Background()
	steps := []ScriptedStep{
		{Tool: "open_page", Arguments: json.RawMessage(
			`{"url":"` + listURL + `","intent":"打开商品列表"}`)},
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"打开详情","expect_url":"/this/never/happens"}`)},
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
		if message.Role == RoleTool && strings.Contains(message.Content, "dry_run_failed") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("the model never saw dry_run_failed")
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
		{Tool: "click", Arguments: json.RawMessage(
			`{"hint":"Widget","intent":"打开详情","expect_url":"/this/never/happens"}`)},
		{Tool: "finish_case", Arguments: json.RawMessage(`{"name":"注定失败"}`)},
		{Final: "我无法让它通过。"},
	}
	h := newHarness(t, steps)

	run, err := h.store.CreateRun(ctx, "打开第一个商品", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
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
	run, err := h.store.CreateRun(ctx, "测一个商品", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
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
		if message.Role == RoleTool && strings.Contains(message.Content, "就测 Widget") {
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
	run, err := database.CreateRun(ctx, "随便测点什么", nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
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
