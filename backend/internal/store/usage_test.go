package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

func TestUsageAccumulatesAndReadsBack(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	_, run, err := store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 未知 run 读出来是零值，不是错误——页面在第一次调用之前就会来读。
	empty, err := store.GetUsage(ctx, run.ID)
	if err != nil {
		t.Fatalf("get usage before any call: %v", err)
	}
	if !empty.IsZero() {
		t.Fatalf("usage before any call = %+v, want zero", empty)
	}

	first, err := store.AddUsage(ctx, run.ID, usage.Call(100, 20, 5, 0))
	if err != nil {
		t.Fatalf("add usage: %v", err)
	}
	if first.ModelCalls != 1 || first.TotalTokens != 120 {
		t.Fatalf("first add = %+v, want 1 call / 120 tokens", first)
	}

	second, err := store.AddUsage(ctx, run.ID, usage.Call(300, 40, 0, 60))
	if err != nil {
		t.Fatalf("add usage again: %v", err)
	}
	if second.ModelCalls != 2 || second.TotalTokens != 460 {
		t.Fatalf("second add = %+v, want 2 calls / 460 tokens", second)
	}
	if second.CachedTokens != 60 {
		t.Fatalf("cached = %d, want 60", second.CachedTokens)
	}

	reread, err := store.GetUsage(ctx, run.ID)
	if err != nil {
		t.Fatalf("re-read usage: %v", err)
	}
	if reread != second {
		t.Fatalf("re-read = %+v, want %+v", reread, second)
	}
}

// 用量是"每个 run 一本账"，不能互相污染。
func TestUsageIsPerRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	_, first, _ := store.CreateSession(ctx, "第一个")
	_, second, _ := store.CreateSession(ctx, "第二个")

	if _, err := store.AddUsage(ctx, first.ID, usage.Call(500, 50, 0, 0)); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	other, err := store.GetUsage(ctx, second.ID)
	if err != nil {
		t.Fatalf("get usage: %v", err)
	}
	if !other.IsZero() {
		t.Fatalf("the second run must not inherit usage: %+v", other)
	}
}

// 旧库（没有 model_usage 表）打开时必须自动建表，否则升级即崩。
func TestOpenAddsUsageTableToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "loop.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, run, err := first.CreateSession(ctx, "旧库里的目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := first.AddUsage(ctx, run.ID, usage.Call(10, 1, 0, 0)); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	spent, err := reopened.GetUsage(ctx, run.ID)
	if err != nil {
		t.Fatalf("get usage after reopen: %v", err)
	}
	if spent.TotalTokens != 11 {
		t.Fatalf("usage after reopen = %+v, want 11 tokens", spent)
	}
}
