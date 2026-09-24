package usage

import "testing"

func TestCallSeparatesFreshPromptTokens(t *testing.T) {
	got := Call(1000, 100, 20, 800)
	if got.FreshPromptTokens != 200 || got.FreshTotalTokens != 300 {
		t.Fatalf("fresh usage = %+v", got)
	}
}

func TestFreshPromptNeverGoesNegative(t *testing.T) {
	got := Call(100, 10, 0, 120)
	if got.FreshPromptTokens != 0 || got.FreshTotalTokens != 10 {
		t.Fatalf("fresh usage = %+v", got)
	}
}

func TestCallNormalizesTotal(t *testing.T) {
	// 方舟会返回 total，但有的提供方只给 prompt/completion，不能因此把成本算成 0。
	value := Call(100, 20, 15, 0)
	if value.TotalTokens != 120 {
		t.Fatalf("total = %d, want 120", value.TotalTokens)
	}
	if value.ModelCalls != 1 {
		t.Fatalf("model_calls = %d, want 1", value.ModelCalls)
	}
	if value.ReasoningTokens != 15 {
		t.Fatalf("reasoning = %d, want 15", value.ReasoningTokens)
	}
}

func TestCallKeepsProviderTotalWhenGiven(t *testing.T) {
	value := Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 99}.Normalize()
	if value.TotalTokens != 99 {
		t.Fatalf("an explicit total must win, got %d", value.TotalTokens)
	}
}

func TestAddAccumulatesEveryField(t *testing.T) {
	total := Call(100, 20, 15, 0).Add(Call(200, 30, 0, 80))
	want := Usage{
		ModelCalls: 2, PromptTokens: 300, CompletionTokens: 50,
		TotalTokens: 350, FreshPromptTokens: 220, FreshTotalTokens: 270,
		ReasoningTokens: 15, CachedTokens: 80,
	}
	if total != want {
		t.Fatalf("total = %+v, want %+v", total, want)
	}
}

func TestAddOfZeroIsIdentity(t *testing.T) {
	base := Call(10, 5, 0, 0)
	if got := base.Add(Usage{}); got != base {
		t.Fatalf("adding zero changed the value: %+v", got)
	}
}

func TestIsZero(t *testing.T) {
	if !(Usage{}).IsZero() {
		t.Fatal("the zero value must be zero")
	}
	if Call(1, 0, 0, 0).IsZero() {
		t.Fatal("a call with tokens is not zero")
	}
}
