package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/store"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
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

func newHarnessWithUsageLimits(
	t *testing.T,
	llm LLM,
	maxTotalTokens, maxFreshTotalTokens, maxPromptTokensPerCall int,
) *harness {
	t.Helper()
	h := newHarnessWithLLM(t, llm, maxTotalTokens)
	h.runtime = New(Config{
		Store:                  h.store,
		Worker:                 h.runtime.client,
		LLM:                    llm,
		MaxModelCalls:          20,
		MaxTotalTokens:         maxTotalTokens,
		MaxFreshTotalTokens:    maxFreshTotalTokens,
		MaxPromptTokensPerCall: maxPromptTokensPerCall,
	})
	return h
}

func startRun(t *testing.T, h *harness, input string) store.Run {
	t.Helper()
	_, run, err := h.store.CreateSession(context.Background(), input)
	if err != nil {
		t.Fatalf("create session: %v", err)
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

func TestFreshTokenBudgetAllowsCachedPromptTokens(t *testing.T) {
	script := happyScript()
	steps := []ScriptedStep{script[0], script[len(script)-1]}
	per := usage.Call(1000, 100, 0, 900)
	h := newHarnessWithUsageLimits(
		t,
		&meteredLLM{inner: NewScriptedLLM(steps), per: per},
		0,
		500,
		0,
	)
	run := startRun(t, h, "打开商品列表")

	if err := h.runtime.Plan(context.Background(), run); err != nil {
		t.Fatalf("two cached calls must fit the fresh-token budget: %v", err)
	}
	spent, err := h.store.GetUsage(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}
	if spent.ModelCalls != 2 || spent.TotalTokens != 2200 || spent.FreshTotalTokens != 400 {
		t.Fatalf("usage = %+v, want 2 calls / 2200 raw / 400 fresh tokens", spent)
	}
}

func TestFreshTokenBudgetStopsTheRun(t *testing.T) {
	per := usage.Call(1000, 100, 0, 800)
	h := newHarnessWithUsageLimits(
		t,
		&meteredLLM{inner: NewScriptedLLM(happyScript()), per: per},
		0,
		500,
		0,
	)
	run := startRun(t, h, "打开第一个商品的详情页")

	err := h.runtime.Plan(context.Background(), run)
	if err == nil {
		t.Fatal("the run must fail once the fresh-token budget is exceeded")
	}
	if !strings.Contains(err.Error(), "LOOP_MAX_FRESH_TOTAL_TOKENS") {
		t.Fatalf("error must name the fresh-token limit: %v", err)
	}

	spent, getErr := h.store.GetUsage(context.Background(), run.ID)
	if getErr != nil {
		t.Fatalf("get usage: %v", getErr)
	}
	if spent.ModelCalls != 2 || spent.FreshTotalTokens != 600 {
		t.Fatalf("usage = %+v, want the over-budget call recorded", spent)
	}
}

func TestPromptTokenPerCallLimitStopsTheRun(t *testing.T) {
	per := usage.Call(31000, 100, 0, 30000)
	h := newHarnessWithUsageLimits(
		t,
		&meteredLLM{inner: NewScriptedLLM(happyScript()), per: per},
		0,
		0,
		30000,
	)
	run := startRun(t, h, "打开第一个商品的详情页")

	err := h.runtime.Plan(context.Background(), run)
	if err == nil {
		t.Fatal("a prompt over the per-call limit must fail the run")
	}
	if !strings.Contains(err.Error(), "LOOP_MAX_PROMPT_TOKENS_PER_CALL") {
		t.Fatalf("error must name the per-call prompt limit: %v", err)
	}

	spent, getErr := h.store.GetUsage(context.Background(), run.ID)
	if getErr != nil {
		t.Fatalf("get usage: %v", getErr)
	}
	if spent.ModelCalls != 1 || spent.PromptTokens != 31000 {
		t.Fatalf("usage = %+v, want the rejected call recorded", spent)
	}
}

func TestRecordUsageFailsClosedWhenAccountingFails(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "loop.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	tests := []struct {
		name      string
		spent     usage.Usage
		wantError string
	}{
		{
			name:      "usage within the per-call limit",
			spent:     usage.Call(1000, 100, 0, 0),
			wantError: "模型用量记账失败",
		},
		{
			name:      "prompt over the per-call limit",
			spent:     usage.Call(31000, 100, 0, 0),
			wantError: "LOOP_MAX_PROMPT_TOKENS_PER_CALL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := New(Config{
				Store:                  database,
				MaxPromptTokensPerCall: 30000,
			})

			err := runtime.recordUsage(context.Background(), "run-accounting-failure", tt.spent)
			if err == nil {
				t.Fatal("accounting failure must stop the run")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.wantError)
			}
		})
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
			var payload struct {
				Call  map[string]any `json:"call"`
				Total map[string]any `json:"total"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode model_usage event: %v", err)
			}
			for _, counter := range []string{
				"prompt_tokens",
				"completion_tokens",
				"total_tokens",
				"reasoning_tokens",
				"cached_tokens",
				"fresh_prompt_tokens",
				"fresh_total_tokens",
			} {
				if _, ok := payload.Call[counter]; !ok {
					t.Fatalf("model_usage call is missing %q: %s", counter, event.Payload)
				}
				if _, ok := payload.Total[counter]; !ok {
					t.Fatalf("model_usage total is missing %q: %s", counter, event.Payload)
				}
			}
		}
	}
	if count != len(steps) {
		t.Fatalf("model_usage events = %d, want %d", count, len(steps))
	}
}
