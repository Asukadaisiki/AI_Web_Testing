package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// Run 是一次闭环执行。
type Run struct {
	ID          string    `json:"id"`
	Input       string    `json:"input"`
	Status      string    `json:"status"`
	ParentRunID *string   `json:"parent_run_id"`
	Error       *string   `json:"error"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateRun 新建一次 run。
func (s *Store) CreateRun(ctx context.Context, input string, parentRunID *string) (Run, error) {
	now := time.Now().UTC()
	run := Run{
		ID:          NewID("run"),
		Input:       input,
		Status:      StatusPlanning,
		ParentRunID: parentRunID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO runs (id, input, status, parent_run_id, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, NULL, ?, ?)`,
		run.ID, run.Input, run.Status, run.ParentRunID, formatTime(now), formatTime(now),
	); err != nil {
		return Run{}, fmt.Errorf("insert run: %w", err)
	}
	return run, nil
}

// GetRun 读取一次 run。
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, input, status, parent_run_id, error, created_at, updated_at FROM runs WHERE id = ?`,
		id,
	)
	return scanRun(row)
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

// ListRuns 按创建时间倒序列出 run。
func (s *Store) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, input, status, parent_run_id, error, created_at, updated_at
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
		parentRunID sql.NullString
		runErr      sql.NullString
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(
		&run.ID, &run.Input, &run.Status, &parentRunID, &runErr, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("scan run: %w", err)
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
