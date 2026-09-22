package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// 建会话时必须同时产出第 1 轮，且两者互相指认（CONTRACT §9.1）。
func TestCreateSessionAlsoCreatesFirstRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, run, err := store.CreateSession(ctx, "把 Blue Top 加入购物车")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.Goal != "把 Blue Top 加入购物车" {
		t.Fatalf("goal = %q", session.Goal)
	}
	if run.ParentRunID != nil {
		t.Fatalf("first run must have no parent, got %v", *run.ParentRunID)
	}
	if run.SessionID == nil || *run.SessionID != session.ID {
		t.Fatalf("run.SessionID = %v, want %q", run.SessionID, session.ID)
	}

	reloaded, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if reloaded.RunCount != 1 {
		t.Fatalf("run_count = %d, want 1", reloaded.RunCount)
	}
	if reloaded.Status != StatusPlanning {
		t.Fatalf("status = %q, want %q", reloaded.Status, StatusPlanning)
	}
}

// 目标为空不建会话——否则会留下没有目标的空会话。
func TestCreateSessionRejectsEmptyGoal(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	if _, _, err := store.CreateSession(ctx, "   "); err == nil {
		t.Fatal("an empty goal must be rejected")
	}
}

// 回灌的下一轮必须落进同一个会话，且带上 parent_run_id。
func TestFollowUpRunStaysInTheSameSession(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, first, err := store.CreateSession(ctx, "打开商品列表")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	second, err := store.CreateRun(ctx, session.ID, "换成搜索 Blue Top", &first.ID)
	if err != nil {
		t.Fatalf("create follow-up run: %v", err)
	}
	if second.SessionID == nil || *second.SessionID != session.ID {
		t.Fatalf("follow-up run session = %v, want %q", second.SessionID, session.ID)
	}
	if second.ParentRunID == nil || *second.ParentRunID != first.ID {
		t.Fatalf("parent = %v, want %q", second.ParentRunID, first.ID)
	}

	runs, err := store.ListRunsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("list runs by session: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runs))
	}
	// 第 1 轮在前：会话是按轮次顺序读的。
	if runs[0].ID != first.ID || runs[1].ID != second.ID {
		t.Fatalf("run order = [%s %s], want [%s %s]", runs[0].ID, runs[1].ID, first.ID, second.ID)
	}

	reloaded, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if reloaded.RunCount != 2 {
		t.Fatalf("run_count = %d, want 2", reloaded.RunCount)
	}
}

// 会话的用量 = 它全部轮次之和；别的会话不能算进来。
func TestSessionUsageSumsAllRuns(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, first, err := store.CreateSession(ctx, "目标一")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	second, err := store.CreateRun(ctx, session.ID, "回灌一轮", &first.ID)
	if err != nil {
		t.Fatalf("create follow-up: %v", err)
	}
	other, _, err := store.CreateSession(ctx, "目标二")
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}

	if _, err := store.AddUsage(ctx, first.ID, usage.Call(1000, 100, 20, 500)); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	if _, err := store.AddUsage(ctx, second.ID, usage.Call(2000, 200, 0, 0)); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	// 别的会话的账，绝不能算进本会话。
	otherRun, err := store.CreateRun(ctx, other.ID, "别的会话", nil)
	if err != nil {
		t.Fatalf("create run in other session: %v", err)
	}
	if _, err := store.AddUsage(ctx, otherRun.ID, usage.Call(9999, 9999, 0, 0)); err != nil {
		t.Fatalf("add usage: %v", err)
	}

	total, err := store.SessionUsage(ctx, session.ID)
	if err != nil {
		t.Fatalf("session usage: %v", err)
	}
	if total.ModelCalls != 2 || total.TotalTokens != 3300 {
		t.Fatalf("session usage = %+v, want 2 calls / 3300 tokens", total)
	}
	if total.CachedTokens != 500 {
		t.Fatalf("cached = %d, want 500", total.CachedTokens)
	}

	sessions, err := store.ListSessions(ctx, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	byID := map[string]Session{}
	for _, item := range sessions {
		byID[item.ID] = item
	}
	if got := byID[session.ID]; got.Usage.TotalTokens != 3300 || got.RunCount != 2 {
		t.Fatalf("listed session = %+v, want 3300 tokens / 2 runs", got)
	}
	// 别的会话：1 轮来自 CreateSession，另 1 轮是后面补的，共 2 轮。
	if got := byID[other.ID]; got.Usage.TotalTokens != 19998 || got.RunCount != 2 {
		t.Fatalf("listed other session = %+v, want 19998 tokens / 2 runs", got)
	}
}

// 会话状态是"最新一轮"的状态，不是第一轮的。
func TestSessionStatusFollowsLatestRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, first, err := store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.UpdateRunStatus(ctx, first.ID, StatusCompleted, nil); err != nil {
		t.Fatalf("update first: %v", err)
	}
	second, err := store.CreateRun(ctx, session.ID, "第二轮", &first.ID)
	if err != nil {
		t.Fatalf("create follow-up: %v", err)
	}

	reloaded, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	// 第 2 轮刚建出来是 planning，比第 1 轮的 completed 新。
	if reloaded.Status != StatusPlanning {
		t.Fatalf("status = %q, want %q", reloaded.Status, StatusPlanning)
	}

	if err := store.UpdateRunStatus(ctx, second.ID, StatusFailed, nil); err != nil {
		t.Fatalf("update second: %v", err)
	}
	reloaded, err = store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if reloaded.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", reloaded.Status, StatusFailed)
	}
}

// case 同时挂在会话与轮次上（CONTRACT §9.1）。
func TestCaseIsBoundToSessionAndRun(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	session, run, err := store.CreateSession(ctx, "目标")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	step, err := contract.DeriveGotoStep(0, "打开列表", "https://shop.test/products")
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	artifact := contract.Case{
		CaseVersion: contract.CaseVersion,
		Name:        "列表",
		Goal:        "目标",
		BaseURL:     "https://shop.test",
		Steps:       []contract.Step{step},
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	saved, err := store.SaveCase(ctx, session.ID, run.ID, artifact)
	if err != nil {
		t.Fatalf("save case: %v", err)
	}
	if saved.SessionID == nil || *saved.SessionID != session.ID {
		t.Fatalf("case session = %v, want %q", saved.SessionID, session.ID)
	}
	if saved.RunID != run.ID {
		t.Fatalf("case run = %q, want %q", saved.RunID, run.ID)
	}
}

// 不存在的会话不能建轮次——否则会出现没有容器的孤儿 run。
func TestCreateRunRejectsUnknownSession(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	if _, err := store.CreateRun(ctx, "sess_missing", "目标", nil); err == nil {
		t.Fatal("a run must not be created in an unknown session")
	}
	if _, err := store.CreateRun(ctx, "", "目标", nil); err == nil {
		t.Fatal("a run must not be created without a session")
	}
}

// 旧库（runs / cases 还没有 session_id 列）打开时必须自动补列，否则升级即崩。
//
// CREATE TABLE IF NOT EXISTS 不会补列，所以这里手写旧 schema 来验证 ensureColumn。
func TestOpenAddsSessionColumnsToAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE runs (
		   id TEXT PRIMARY KEY, input TEXT NOT NULL, status TEXT NOT NULL,
		   parent_run_id TEXT, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE cases (
		   id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL UNIQUE,
		   content_hash TEXT NOT NULL, payload_json TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO runs (id, input, status, created_at, updated_at)
		 VALUES ('run_old', '旧目标', 'completed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy db with the current schema: %v", err)
	}
	defer store.Close()

	for _, table := range []string{"runs", "cases"} {
		if !hasColumn(t, store, table, "session_id") {
			t.Fatalf("%s.session_id was not added by the migration", table)
		}
	}

	// 旧行还在，session_id 为空——旧数据不该被删，也不该被瞎猜一个会话。
	old, err := store.GetRun(ctx, "run_old")
	if err != nil {
		t.Fatalf("the legacy run must survive the migration: %v", err)
	}
	if old.SessionID != nil {
		t.Fatalf("legacy run session = %v, want nil", *old.SessionID)
	}

	// 迁移之后新数据要能正常写。
	session, run, err := store.CreateSession(ctx, "迁移后的目标")
	if err != nil {
		t.Fatalf("create session after migration: %v", err)
	}
	if run.SessionID == nil || *run.SessionID != session.ID {
		t.Fatalf("run.SessionID = %v, want %q", run.SessionID, session.ID)
	}
}

func hasColumn(t *testing.T, store *Store, table, column string) bool {
	t.Helper()
	rows, err := store.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}
