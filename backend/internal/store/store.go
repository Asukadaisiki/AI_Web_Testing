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

// schema 是全部表定义。SQLite 单库，不需要增量迁移框架。
const schema = `
CREATE TABLE IF NOT EXISTS runs (
  id            TEXT PRIMARY KEY,
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
