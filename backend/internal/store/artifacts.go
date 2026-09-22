package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// CaseArtifact 是不可变的 case 工件，按内容哈希寻址。
//
// SessionID 让 case / DSL 直接挂在会话上（CONTRACT §9）：RunID 回答"哪一轮产出的"，
// SessionID 回答"属于哪个会话"，回灌换轮次时后者不变。
type CaseArtifact struct {
	ID          int64           `json:"id"`
	SessionID   *string         `json:"session_id"`
	RunID       string          `json:"run_id"`
	ContentHash string          `json:"content_hash"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Execution 是一次执行的落库记录。
type Execution struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	CaseID     int64           `json:"case_id"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	CreatedAt  time.Time       `json:"created_at"`
}

// Signal 是一条失败信号。
type Signal struct {
	ID          int64     `json:"id"`
	RunID       string    `json:"run_id"`
	ExecutionID string    `json:"execution_id"`
	StepIndex   int       `json:"step_index"`
	Kind        string    `json:"kind"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"created_at"`
}

// FeedbackCandidate 是一条失败回灌候选。
type FeedbackCandidate struct {
	ID            int64     `json:"id"`
	RunID         string    `json:"run_id"`
	SignalKind    string    `json:"signal_kind"`
	ProposedInput string    `json:"proposed_input"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// SaveCase 保存 case 工件；同一 run 重复保存会覆盖（重新规划时）。
//
// sessionID 由调用方从 run 上取，落库后 case 就同时挂在会话与轮次上。
func (s *Store) SaveCase(
	ctx context.Context, sessionID, runID string, artifact contract.Case,
) (CaseArtifact, error) {
	payload := artifact.JSON()
	now := time.Now().UTC()
	var session any
	if sessionID != "" {
		session = sessionID
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO cases (session_id, run_id, content_hash, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (run_id) DO UPDATE SET
		   session_id = excluded.session_id,
		   content_hash = excluded.content_hash,
		   payload_json = excluded.payload_json,
		   created_at = excluded.created_at`,
		session, runID, artifact.ContentHash(), string(payload), formatTime(now),
	); err != nil {
		return CaseArtifact{}, fmt.Errorf("save case: %w", err)
	}
	return s.GetCase(ctx, runID)
}

// GetCase 读取某个 run 的 case 工件。
func (s *Store) GetCase(ctx context.Context, runID string) (CaseArtifact, error) {
	var (
		artifact  CaseArtifact
		sessionID sql.NullString
		payload   string
		createdAt string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, session_id, run_id, content_hash, payload_json, created_at
		 FROM cases WHERE run_id = ?`,
		runID,
	).Scan(&artifact.ID, &sessionID, &artifact.RunID, &artifact.ContentHash, &payload, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseArtifact{}, ErrNotFound
		}
		return CaseArtifact{}, fmt.Errorf("get case: %w", err)
	}
	if sessionID.Valid {
		value := sessionID.String
		artifact.SessionID = &value
	}
	artifact.Payload = json.RawMessage(payload)
	artifact.CreatedAt = parseTime(createdAt)
	return artifact, nil
}

// ApproveCase 记录审批。重复审批幂等。
func (s *Store) ApproveCase(ctx context.Context, caseID int64, approvedBy string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO case_approvals (case_id, approved_by, created_at) VALUES (?, ?, ?)
		 ON CONFLICT (case_id) DO NOTHING`,
		caseID, approvedBy, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("approve case: %w", err)
	}
	return nil
}

// IsCaseApproved 判断工件是否已审批。
func (s *Store) IsCaseApproved(ctx context.Context, caseID int64) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM case_approvals WHERE case_id = ?`, caseID,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("check approval: %w", err)
	}
	return count > 0, nil
}

// SaveExecution 保存执行结果与逐步骤证据。
func (s *Store) SaveExecution(
	ctx context.Context, runID string, caseID int64, result contract.ExecutionResult,
) (Execution, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return Execution{}, fmt.Errorf("marshal execution: %w", err)
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO executions (id, run_id, case_id, status, result_json, started_at, finished_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (run_id) DO UPDATE SET
		   status = excluded.status,
		   result_json = excluded.result_json,
		   started_at = excluded.started_at,
		   finished_at = excluded.finished_at`,
		result.ExecutionID, runID, caseID, string(result.Status), string(raw),
		formatTime(result.StartedAt), formatTime(result.FinishedAt), formatTime(now),
	); err != nil {
		return Execution{}, fmt.Errorf("insert execution: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx, `DELETE FROM execution_steps WHERE execution_id = ?`, result.ExecutionID,
	); err != nil {
		return Execution{}, fmt.Errorf("clear execution steps: %w", err)
	}
	for _, step := range result.Steps {
		evidence, err := json.Marshal(step.Evidence)
		if err != nil {
			return Execution{}, fmt.Errorf("marshal evidence: %w", err)
		}
		var stepErr *string
		if step.Error != nil {
			message := string(step.Error.Kind) + ": " + step.Error.Message
			stepErr = &message
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO execution_steps (execution_id, step_index, action, status, evidence_json, error)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			result.ExecutionID, step.Index, string(step.Action), step.Status, string(evidence), stepErr,
		); err != nil {
			return Execution{}, fmt.Errorf("insert execution step: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, err
	}
	return s.GetExecution(ctx, runID)
}

// GetExecution 读取某个 run 的执行记录。
func (s *Store) GetExecution(ctx context.Context, runID string) (Execution, error) {
	var (
		execution  Execution
		result     string
		startedAt  sql.NullString
		finishedAt sql.NullString
		createdAt  string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, run_id, case_id, status, result_json, started_at, finished_at, created_at
		 FROM executions WHERE run_id = ?`,
		runID,
	).Scan(
		&execution.ID, &execution.RunID, &execution.CaseID, &execution.Status,
		&result, &startedAt, &finishedAt, &createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Execution{}, ErrNotFound
		}
		return Execution{}, fmt.Errorf("get execution: %w", err)
	}
	execution.Result = json.RawMessage(result)
	execution.CreatedAt = parseTime(createdAt)
	if startedAt.Valid {
		value := parseTime(startedAt.String)
		execution.StartedAt = &value
	}
	if finishedAt.Valid {
		value := parseTime(finishedAt.String)
		execution.FinishedAt = &value
	}
	return execution, nil
}

// ReplaceSignals 用最新一次执行的信号覆盖该 run 的信号。
func (s *Store) ReplaceSignals(
	ctx context.Context, runID, executionID string, signals []Signal,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM report_signals WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("clear signals: %w", err)
	}
	now := time.Now().UTC()
	for _, signal := range signals {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO report_signals (run_id, execution_id, step_index, kind, message, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			runID, executionID, signal.StepIndex, signal.Kind, signal.Message, formatTime(now),
		); err != nil {
			return fmt.Errorf("insert signal: %w", err)
		}
	}
	return tx.Commit()
}

// ListSignals 读取某个 run 的失败信号。
func (s *Store) ListSignals(ctx context.Context, runID string) ([]Signal, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, run_id, execution_id, step_index, kind, message, created_at
		 FROM report_signals WHERE run_id = ? ORDER BY step_index, id`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list signals: %w", err)
	}
	defer rows.Close()
	signals := make([]Signal, 0, 8)
	for rows.Next() {
		var (
			signal    Signal
			createdAt string
		)
		if err := rows.Scan(
			&signal.ID, &signal.RunID, &signal.ExecutionID, &signal.StepIndex,
			&signal.Kind, &signal.Message, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		signal.CreatedAt = parseTime(createdAt)
		signals = append(signals, signal)
	}
	return signals, rows.Err()
}

// ReplaceFeedback 用最新一批候选覆盖该 run 的候选。
func (s *Store) ReplaceFeedback(
	ctx context.Context, runID string, candidates []FeedbackCandidate,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM feedback_candidates WHERE run_id = ?`, runID); err != nil {
		return fmt.Errorf("clear feedback: %w", err)
	}
	now := time.Now().UTC()
	for _, candidate := range candidates {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO feedback_candidates (run_id, signal_kind, proposed_input, status, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			runID, candidate.SignalKind, candidate.ProposedInput, "pending", formatTime(now),
		); err != nil {
			return fmt.Errorf("insert feedback candidate: %w", err)
		}
	}
	return tx.Commit()
}

// ListFeedback 读取某个 run 的回灌候选。
func (s *Store) ListFeedback(ctx context.Context, runID string) ([]FeedbackCandidate, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, run_id, signal_kind, proposed_input, status, created_at
		 FROM feedback_candidates WHERE run_id = ? ORDER BY id`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer rows.Close()
	candidates := make([]FeedbackCandidate, 0, 4)
	for rows.Next() {
		var (
			candidate FeedbackCandidate
			createdAt string
		)
		if err := rows.Scan(
			&candidate.ID, &candidate.RunID, &candidate.SignalKind,
			&candidate.ProposedInput, &candidate.Status, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback candidate: %w", err)
		}
		candidate.CreatedAt = parseTime(createdAt)
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

// GetFeedback 读取单条候选。
func (s *Store) GetFeedback(ctx context.Context, id int64) (FeedbackCandidate, error) {
	var (
		candidate FeedbackCandidate
		createdAt string
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, run_id, signal_kind, proposed_input, status, created_at
		 FROM feedback_candidates WHERE id = ?`,
		id,
	).Scan(
		&candidate.ID, &candidate.RunID, &candidate.SignalKind,
		&candidate.ProposedInput, &candidate.Status, &createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FeedbackCandidate{}, ErrNotFound
		}
		return FeedbackCandidate{}, fmt.Errorf("get feedback candidate: %w", err)
	}
	candidate.CreatedAt = parseTime(createdAt)
	return candidate, nil
}

// MarkFeedbackUsed 标记候选已被采纳。
func (s *Store) MarkFeedbackUsed(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(
		ctx, `UPDATE feedback_candidates SET status = 'used' WHERE id = ?`, id,
	); err != nil {
		return fmt.Errorf("mark feedback used: %w", err)
	}
	return nil
}
