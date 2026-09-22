package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/usage"
)

// meteredLLM 在脚本模型外面套一层"报用量"，用来在离线测试里造出真实成本。
type meteredLLM struct {
	inner *ScriptedLLM
	per   usage.Usage
}

func (m *meteredLLM) Label() string { return "metered" }

func (m *meteredLLM) Next(ctx context.Context, messages []Message) (Message, usage.Usage, error) {
	message, _, err := m.inner.Next(ctx, messages)
	if err != nil {
		return Message{}, usage.Usage{}, err
	}
	return message, m.per, nil
}

// 每次调用 1000 输入 + 100 输出（其中 20 是思考 token）。
func meteredPerCall() usage.Usage { return usage.Call(1000, 100, 20, 0) }

func startRun(t *testing.T, h *harness, input string) store.Run {
	t.Helper()
	run, err := h.store.CreateRun(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	return run
}

// 每一次模型调用的用量都要落到该 run 的账上，且总额等于各次之和。
func TestUsageIsAccumulatedAcrossModelCalls(t *testing.T) {
	steps := happyScript() // 4 次模型调用
	h := newHarnessWithLLM(t, &meteredLLM{inner: NewScriptedLLM(steps), per: meteredPerCall()}, 0)
	run := startRun(t, h, "打开第一个商品的详情页")

	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("plan: %v", err)
	}

	spent, err := h.store.GetUsage(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}
	if spent.ModelCalls != len(steps) {
		t.Fatalf("model_calls = %d, want %d", spent.ModelCalls, len(steps))
	}
	wantPrompt := 1000 * len(steps)
	wantCompletion := 100 * len(steps)
	if spent.PromptTokens != wantPrompt || spent.CompletionTokens != wantCompletion {
		t.Fatalf("usage = %+v, want prompt=%d completion=%d", spent, wantPrompt, wantCompletion)
	}
	if spent.TotalTokens != wantPrompt+wantCompletion {
		t.Fatalf("total = %d, want %d", spent.TotalTokens, wantPrompt+wantCompletion)
	}
	if spent.ReasoningTokens != 20*len(steps) {
		t.Fatalf("reasoning = %d, want %d", spent.ReasoningTokens, 20*len(steps))
	}
}

// 用量必须按 run 分开记账，不能串到别的 run 上。
func TestUsageIsScopedToItsRun(t *testing.T) {
	h := newHarnessWithLLM(t, &meteredLLM{inner: NewScriptedLLM(happyScript()), per: meteredPerCall()}, 0)
	ctx := context.Background()

	first := startRun(t, h, "第一个目标")
	if err := h.runtime.Plan(ctx, first); err != nil {
		t.Fatalf("plan first: %v", err)
	}
	second := startRun(t, h, "第二个目标")

	spent, err := h.store.GetUsage(ctx, second.ID)
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}
	if !spent.IsZero() {
		t.Fatalf("a run that has not called the model must have zero usage, got %+v", spent)
	}
}

// 成本熔断：超预算立刻中止，并且把已经花掉的部分留在账上。
func TestTokenBudgetStopsTheRun(t *testing.T) {
	// 每次 1100 token，预算 1500：第 2 次调用后必然超。
	h := newHarnessWithLLM(t, &meteredLLM{inner: NewScriptedLLM(happyScript()), per: meteredPerCall()}, 1500)
	run := startRun(t, h, "打开第一个商品的详情页")

	err := h.runtime.Plan(context.Background(), run)
	if err == nil {
		t.Fatal("the run must fail once the token budget is exceeded")
	}
	if !strings.Contains(err.Error(), "token 预算用尽") {
		t.Fatalf("error must say the budget ran out: %v", err)
	}
	// 消息要给出"花了多少 / 上限多少"，否则用户不知道该把上限调到多少。
	if !strings.Contains(err.Error(), "1500") || !strings.Contains(err.Error(), "LOOP_MAX_TOTAL_TOKENS") {
		t.Fatalf("error must state the limit and how to raise it: %v", err)
	}

	reloaded, err := h.store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if reloaded.Status != store.StatusFailed {
		t.Fatalf("status = %q, want failed（run 本身中止，不是 case 失败）", reloaded.Status)
	}

	spent, err := h.store.GetUsage(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}
	// 超预算的那一次调用也已经花掉了，必须记账。
	if spent.ModelCalls != 2 || spent.TotalTokens != 2200 {
		t.Fatalf("usage = %+v, want 2 calls / 2200 tokens recorded", spent)
	}
}

// 预算够用时不能误伤。
func TestTokenBudgetAllowsARunWithinLimit(t *testing.T) {
	h := newHarnessWithLLM(t, &meteredLLM{inner: NewScriptedLLM(happyScript()), per: meteredPerCall()}, 100000)
	run := startRun(t, h, "打开第一个商品的详情页")

	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("plan: %v", err)
	}
	reloaded, _ := h.store.GetRun(context.Background(), run.ID)
	if reloaded.Status != store.StatusAwaitingApproval {
		t.Fatalf("status = %q, want awaiting_approval", reloaded.Status)
	}
}

// 脚本模型不花钱：即使把预算设成 1，离线闭环也必须照跑不误。
// 这条是"离线验证可以反复跑"的前提。
func TestScriptedModelIsNotBilled(t *testing.T) {
	h := newHarnessWithLLM(t, NewScriptedLLM(happyScript()), 1)
	run := startRun(t, h, "打开第一个商品的详情页")

	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("the offline scripted model must not be billed: %v", err)
	}
	spent, _ := h.store.GetUsage(context.Background(), run.ID)
	if !spent.IsZero() {
		t.Fatalf("scripted usage = %+v, want zero", spent)
	}
}

// 每次调用都要发一条 model_usage 事件，页面上才能看着成本涨。
func TestModelUsageEventsAreEmitted(t *testing.T) {
	steps := happyScript()
	h := newHarnessWithLLM(t, &meteredLLM{inner: NewScriptedLLM(steps), per: meteredPerCall()}, 0)
	run := startRun(t, h, "打开第一个商品的详情页")

	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("plan: %v", err)
	}
	events, err := h.store.ListEvents(context.Background(), run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	count := 0
	for _, event := range events {
		if event.Type == EventModelUsage {
			count++
		}
	}
	if count != len(steps) {
		t.Fatalf("model_usage events = %d, want %d", count, len(steps))
	}
}
