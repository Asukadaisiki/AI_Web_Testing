// Package store 是 v2 闭环的 SQLite 持久化层。
//
// 只有一种 case 形态（cases.payload_json），执行与报告都读它，没有第二数据源。
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// schema 是全部表定义。SQLite 单库，不需要增量迁移框架；
// 但给已存在的库补列由 ensureColumn 负责（CREATE TABLE IF NOT EXISTS 不会补列）。
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT PRIMARY KEY,
  goal       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
  id            TEXT PRIMARY KEY,
  session_id    TEXT,
  input         TEXT NOT NULL,
  status        TEXT NOT NULL,
  parent_run_id TEXT,
  error         TEXT,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS run_events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id       TEXT NOT NULL,
  seq          INTEGER NOT NULL,
  type         TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  UNIQUE (run_id, seq)
);
CREATE TABLE IF NOT EXISTS cases (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id   TEXT,
  run_id       TEXT NOT NULL UNIQUE,
  content_hash TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at   TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS case_approvals (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  case_id     INTEGER NOT NULL UNIQUE,
  approved_by TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS executions (
  id          TEXT PRIMARY KEY,
  run_id      TEXT NOT NULL UNIQUE,
  case_id     INTEGER NOT NULL,
  status      TEXT NOT NULL,
  result_json TEXT NOT NULL,
  started_at  TEXT,
  finished_at TEXT,
  created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS execution_steps (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id  TEXT NOT NULL,
  step_index    INTEGER NOT NULL,
  action        TEXT NOT NULL,
  status        TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  error         TEXT,
  UNIQUE (execution_id, step_index)
);
CREATE TABLE IF NOT EXISTS report_signals (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id       TEXT NOT NULL,
  execution_id TEXT NOT NULL,
  step_index   INTEGER NOT NULL,
  kind         TEXT NOT NULL,
  message      TEXT NOT NULL,
  created_at   TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS feedback_candidates (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id         TEXT NOT NULL,
  signal_kind    TEXT NOT NULL,
  proposed_input TEXT NOT NULL,
  status         TEXT NOT NULL,
  created_at     TEXT NOT NULL
);
-- 一个 run 一行，按调用增量累加。独立成表而不是给 runs 加列：
-- CREATE TABLE IF NOT EXISTS 对已存在的库也会建出来，不需要列迁移。
CREATE TABLE IF NOT EXISTS model_usage (
  run_id            TEXT PRIMARY KEY,
  model_calls       INTEGER NOT NULL,
  prompt_tokens     INTEGER NOT NULL,
  completion_tokens INTEGER NOT NULL,
  total_tokens      INTEGER NOT NULL,
  reasoning_tokens  INTEGER NOT NULL,
  cached_tokens     INTEGER NOT NULL,
  updated_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_events_run ON run_events (run_id, seq);
CREATE INDEX IF NOT EXISTS idx_signals_run ON report_signals (run_id);
CREATE INDEX IF NOT EXISTS idx_feedback_run ON feedback_candidates (run_id);
`

// sessionIndexes 依赖 session_id 列，所以必须在 ensureColumn 之后建。
//
// 放在 schema 里会在旧库上炸：旧库的 runs 还没有 session_id，
// 而 CREATE INDEX 会先于补列执行，直接报 "no such column: session_id"。
const sessionIndexes = `
CREATE INDEX IF NOT EXISTS idx_runs_session ON runs (session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cases_session ON cases (session_id);
`

// ensureColumn 给已存在的表补一列；列已存在时是空操作。
//
// 为什么需要它：`CREATE TABLE IF NOT EXISTS` 对已存在的库不会补列，
// 而 session_id 是给 runs / cases 加的真实列（不是独立表能替代的）。
// SQLite 没有 `ADD COLUMN IF NOT EXISTS`，所以先用 PRAGMA 查一遍。
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("read table_info(%s): %w", table, err)
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
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read table_info(%s): %w", table, err)
	}
	if _, err := db.Exec(
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition),
	); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// Store 是 SQLite 存储。
type Store struct {
	db     *sql.DB
	broker *Broker
}

// Open 打开（必要时创建）数据库并应用 schema。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite 只有一个写者，收敛到单连接避免 SQLITE_BUSY。
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// 已存在的库：CREATE TABLE IF NOT EXISTS 不会补列，这里补上。
	for _, migration := range []struct{ table, column, definition string }{
		{"runs", "session_id", "TEXT"},
		{"cases", "session_id", "TEXT"},
	} {
		if err := ensureColumn(db, migration.table, migration.column, migration.definition); err != nil {
			db.Close()
			return nil, err
		}
	}
	// 补列之后才建依赖它的索引。
	if _, err := db.Exec(sessionIndexes); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply session indexes: %w", err)
	}
	return &Store{db: db, broker: NewBroker()}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// Broker 暴露给 SSE 使用。
func (s *Store) Broker() *Broker { return s.broker }

// NewID 生成带前缀的随机 id。
func NewID(prefix string) string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// Event 是一条 run 事件，SSE 按 seq 重放。
type Event struct {
	RunID     string          `json:"-"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Broker 是进程内的事件广播器。
type Broker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan Event]struct{}
}

// NewBroker 创建广播器。
func NewBroker() *Broker {
	return &Broker{subscribers: map[string]map[chan Event]struct{}{}}
}

// Subscribe 订阅某个 run 的事件，返回取消函数。
func (b *Broker) Subscribe(runID string) (<-chan Event, func()) {
	channel := make(chan Event, 64)
	b.mu.Lock()
	if b.subscribers[runID] == nil {
		b.subscribers[runID] = map[chan Event]struct{}{}
	}
	b.subscribers[runID][channel] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if subscribers, ok := b.subscribers[runID]; ok {
			if _, exists := subscribers[channel]; exists {
				delete(subscribers, channel)
				close(channel)
			}
			if len(subscribers) == 0 {
				delete(b.subscribers, runID)
			}
		}
		b.mu.Unlock()
	}
	return channel, cancel
}

// Publish 广播一条事件，订阅者满则丢弃（SSE 有 seq 重放兜底）。
func (b *Broker) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for channel := range b.subscribers[event.RunID] {
		select {
		case channel <- event:
		default:
		}
	}
}
