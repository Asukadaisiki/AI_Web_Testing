package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// runUsageView 是 GET /api/runs/{id} 里我们关心的部分。
type runUsageView struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Usage  usage.Usage `json:"usage"`
}

// 页面 1 在规划过程中就要显示成本，所以用量必须挂在 run 详情上。
func TestGetRunCarriesModelUsage(t *testing.T) {
	ctx := context.Background()
	ts := newTestServer(t)

	_, run, err := ts.store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := ts.store.AddUsage(ctx, run.ID, usage.Call(1200, 300, 80, 0)); err != nil {
		t.Fatalf("add usage: %v", err)
	}

	status, raw := ts.do(t, http.MethodGet, "/api/runs/"+run.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("get run: %d %s", status, raw)
	}
	view := decode[runUsageView](t, raw)
	if view.Usage.ModelCalls != 1 || view.Usage.TotalTokens != 1500 {
		t.Fatalf("usage = %+v, want 1 call / 1500 tokens", view.Usage)
	}
	if view.Usage.ReasoningTokens != 80 {
		t.Fatalf("reasoning = %d, want 80", view.Usage.ReasoningTokens)
	}
}

// 列表与详情必须是同一个 run 形状：前端不该为"同一个东西的两种 JSON"写两套解析。
func TestListRunsCarriesTheSameUsageShape(t *testing.T) {
	ctx := context.Background()
	ts := newTestServer(t)

	_, run, err := ts.store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := ts.store.AddUsage(ctx, run.ID, usage.Call(700, 70, 0, 30)); err != nil {
		t.Fatalf("add usage: %v", err)
	}

	status, raw := ts.do(t, http.MethodGet, "/api/runs?limit=5", nil)
	if status != http.StatusOK {
		t.Fatalf("list runs: %d %s", status, raw)
	}
	body := decode[struct {
		Runs []runUsageView `json:"runs"`
	}](t, raw)
	if len(body.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(body.Runs))
	}
	if body.Runs[0].Usage.TotalTokens != 770 || body.Runs[0].Usage.CachedTokens != 30 {
		t.Fatalf("list usage = %+v, want 770 tokens / 30 cached", body.Runs[0].Usage)
	}
}

// 还没调用过模型的 run 也必须返回一个 usage 对象（全零），而不是 null——
// 否则前端每处都要写一次空值判断。
func TestGetRunAlwaysReturnsAUsageObject(t *testing.T) {
	ctx := context.Background()
	ts := newTestServer(t)

	_, run, err := ts.store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	status, raw := ts.do(t, http.MethodGet, "/api/runs/"+run.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("get run: %d %s", status, raw)
	}
	// 用 map 而不是结构体解码，才能区分"字段缺失"和"字段为零值"。
	body := decode[map[string]any](t, raw)
	value, ok := body["usage"]
	if !ok {
		t.Fatal("the response must always contain a usage field")
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("usage must be an object, got %T (%v)", value, value)
	}
	for _, key := range []string{
		"model_calls", "prompt_tokens", "completion_tokens",
		"total_tokens", "reasoning_tokens", "cached_tokens",
	} {
		if _, ok := object[key]; !ok {
			t.Fatalf("usage is missing %q: %v", key, object)
		}
	}
}
