package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// run 状态机。planning → awaiting_approval → executing → reporting → completed/failed，
// 另有分支状态 awaiting_input（模型向人提问时）。
const (
	StatusPlanning         = "planning"
	StatusAwaitingInput    = "awaiting_input"
	StatusAwaitingApproval = "awaiting_approval"
	StatusExecuting        = "executing"
	StatusReporting        = "reporting"
	StatusCompleted        = "completed"
	StatusFailed           = "failed"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("not found")

// Run 是一次闭环执行，属于某个会话（§9）。
type Run struct {
	ID          string    `json:"id"`
	SessionID   *string   `json:"session_id"`
	Input       string    `json:"input"`
	Status      string    `json:"status"`
	ParentRunID *string   `json:"parent_run_id"`
	Error       *string   `json:"error"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// execer 让「建会话 + 建首轮 run」能在一个事务里复用同一段插入逻辑。
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertRun 插入一轮 run。
func insertRun(
	ctx context.Context, db execer, sessionID, input string, parentRunID *string,
) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:          NewID("run"),
		Input:       input,
		Status:      StatusPlanning,
		ParentRunID: parentRunID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if sessionID != "" {
		value := sessionID
		run.SessionID = &value
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO runs (id, session_id, input, status, parent_run_id, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
		run.ID, run.SessionID, run.Input, run.Status, run.ParentRunID,
		formatTime(now), formatTime(now),
	); err != nil {
		return Run{}, fmt.Errorf("insert run: %w", err)
	}
	return run, nil
}

// CreateRun 在既有会话里新建一轮 run（回灌链条上的下一轮）。
func (s *Store) CreateRun(
	ctx context.Context, sessionID, input string, parentRunID *string,
) (Run, error) {
	if sessionID == "" {
		return Run{}, fmt.Errorf("create run: session_id is required")
	}
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return Run{}, err
	}
	return insertRun(ctx, s.db, sessionID, input, parentRunID)
}

// GetRun 读取一次 run。
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, input, status, parent_run_id, error, created_at, updated_at
		 FROM runs WHERE id = ?`,
		id,
	)
	return scanRun(row)
}

// ListRunsBySession 按轮次顺序列出一个会话的全部轮次（第 1 轮在前）。
//
// 用 rowid 而不是 id 做 tie-break：同一毫秒内建出来的两轮 created_at 可能逐字相同
// （Windows 时钟精度约 15ms），而 id 是随机 hex，排序会变成随机的。
// rowid 是插入顺序，正好就是轮次顺序。
func (s *Store) ListRunsBySession(ctx context.Context, sessionID string) ([]Run, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, input, status, parent_run_id, error, created_at, updated_at
		 FROM runs WHERE session_id = ? ORDER BY created_at ASC, rowid ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs by session: %w", err)
	}
	defer rows.Close()
	runs := make([]Run, 0, 4)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// UpdateRunStatus 更新状态与错误信息。
func (s *Store) UpdateRunStatus(ctx context.Context, id, status string, runErr *string) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE runs SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, runErr, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecoverInterruptedRuns fails work that depended on the previous process's
// in-memory planner, answer channel, or executor request. Awaiting approval is
// intentionally durable because its case artifact can be approved after restart.
func (s *Store) RecoverInterruptedRuns(ctx context.Context, reason string) (int, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return 0, fmt.Errorf("recover interrupted runs: reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted runs: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, status FROM runs
		 WHERE status IN (?, ?, ?, ?)
		 ORDER BY rowid`,
		StatusPlanning, StatusAwaitingInput, StatusExecuting, StatusReporting,
	)
	if err != nil {
		return 0, fmt.Errorf("list interrupted runs: %w", err)
	}
	type interruptedRun struct {
		id     string
		status string
	}
	interrupted := make([]interruptedRun, 0)
	for rows.Next() {
		var run interruptedRun
		if scanErr := rows.Scan(&run.id, &run.status); scanErr != nil {
			rows.Close()
			return 0, fmt.Errorf("scan interrupted run: %w", scanErr)
		}
		interrupted = append(interrupted, run)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return 0, fmt.Errorf("list interrupted runs: %w", rowsErr)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return 0, fmt.Errorf("close interrupted runs: %w", closeErr)
	}

	now := time.Now().UTC()
	errorPayload, err := json.Marshal(map[string]any{"message": reason})
	if err != nil {
		return 0, fmt.Errorf("marshal recovery error event: %w", err)
	}
	statusPayload, err := json.Marshal(map[string]any{
		"status": StatusFailed,
		"error":  reason,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal recovery status event: %w", err)
	}

	events := make([]Event, 0, len(interrupted)*2)
	recovered := 0
	for _, run := range interrupted {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE runs SET status = ?, error = ?, updated_at = ?
			 WHERE id = ? AND status = ?`,
			StatusFailed, reason, formatTime(now), run.id, run.status,
		)
		if err != nil {
			return 0, fmt.Errorf("fail interrupted run %s: %w", run.id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count recovered run %s: %w", run.id, err)
		}
		if affected == 0 {
			continue
		}

		var nextSeq int64
		if err := tx.QueryRowContext(
			ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = ?`, run.id,
		).Scan(&nextSeq); err != nil {
			return 0, fmt.Errorf("next recovery event seq for %s: %w", run.id, err)
		}
		for offset, event := range []struct {
			eventType string
			payload   []byte
		}{
			{eventType: "error", payload: errorPayload},
			{eventType: "run_status", payload: statusPayload},
		} {
			seq := nextSeq + int64(offset)
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO run_events (run_id, seq, type, payload_json, created_at)
				 VALUES (?, ?, ?, ?, ?)`,
				run.id, seq, event.eventType, string(event.payload), formatTime(now),
			); err != nil {
				return 0, fmt.Errorf("append recovery event for %s: %w", run.id, err)
			}
			events = append(events, Event{
				RunID: run.id, Seq: seq, Type: event.eventType,
				Payload: append(json.RawMessage(nil), event.payload...), CreatedAt: now,
			})
		}
		recovered++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit interrupted run recovery: %w", err)
	}
	for _, event := range events {
		s.broker.Publish(event)
	}
	return recovered, nil
}

// ListRuns 按创建时间倒序列出 run。
func (s *Store) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, input, status, parent_run_id, error, created_at, updated_at
		 FROM runs ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	runs := make([]Run, 0, limit)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (Run, error) {
	var (
		run         Run
		sessionID   sql.NullString
		parentRunID sql.NullString
		runErr      sql.NullString
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(
		&run.ID, &sessionID, &run.Input, &run.Status, &parentRunID, &runErr, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("scan run: %w", err)
	}
	if sessionID.Valid {
		value := sessionID.String
		run.SessionID = &value
	}
	if parentRunID.Valid {
		value := parentRunID.String
		run.ParentRunID = &value
	}
	if runErr.Valid {
		value := runErr.String
		run.Error = &value
	}
	run.CreatedAt = parseTime(createdAt)
	run.UpdatedAt = parseTime(updatedAt)
	return run, nil
}

// AppendEvent 追加一条事件并广播；seq 在 run 内单调递增。
func (s *Store) AppendEvent(ctx context.Context, runID, eventType string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal event payload: %w", err)
	}
	if payload == nil {
		raw = []byte("null")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()

	var nextSeq int64
	if err := tx.QueryRowContext(
		ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM run_events WHERE run_id = ?`, runID,
	).Scan(&nextSeq); err != nil {
		return Event{}, fmt.Errorf("next event seq: %w", err)
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO run_events (run_id, seq, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		runID, nextSeq, eventType, string(raw), formatTime(now),
	); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	event := Event{
		RunID:     runID,
		Seq:       nextSeq,
		Type:      eventType,
		Payload:   raw,
		CreatedAt: now,
	}
	s.broker.Publish(event)
	return event, nil
}

// ListEvents 读取 seq 大于 from 的事件，用于 SSE 断线重放。
func (s *Store) ListEvents(ctx context.Context, runID string, from int64) ([]Event, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT seq, type, payload_json, created_at FROM run_events
		 WHERE run_id = ? AND seq > ? ORDER BY seq`,
		runID, from,
	)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, 32)
	for rows.Next() {
		var (
			event     Event
			payload   string
			createdAt string
		)
		if err := rows.Scan(&event.Seq, &event.Type, &payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		event.RunID = runID
		event.Payload = json.RawMessage(payload)
		event.CreatedAt = parseTime(createdAt)
		events = append(events, event)
	}
	return events, rows.Err()
}
