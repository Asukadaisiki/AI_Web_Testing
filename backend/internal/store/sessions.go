package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// Session 是一个目标 + 它的全部轮次（CONTRACT §9）。
//
// 会话自身**没有状态机**：Status 是它最新一轮 run 的状态，只用于列表展示，
// 不参与任何判断。RunCount / Status / Usage 都是派生字段，不落库。
type Session struct {
	ID        string      `json:"id"`
	Goal      string      `json:"goal"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	RunCount  int         `json:"run_count"`
	Status    string      `json:"status"`
	Usage     usage.Usage `json:"usage"`
}

// CreateSession 建会话，并在同一个事务里产出第 1 轮 run。
//
// 一个会话至少有一轮：目标属于会话而不属于某一轮，但"开始跑"这件事总要落在某一轮上。
func (s *Store) CreateSession(ctx context.Context, goal string) (Session, Run, error) {
	if strings.TrimSpace(goal) == "" {
		return Session{}, Run{}, fmt.Errorf("create session: goal is required")
	}
	now := time.Now().UTC()
	session := Session{
		ID:        NewID("sess"),
		Goal:      goal,
		CreatedAt: now,
		UpdatedAt: now,
		RunCount:  1,
		Status:    StatusPlanning,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, Run{}, fmt.Errorf("begin create session: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO sessions (id, goal, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		session.ID, session.Goal, formatTime(now), formatTime(now),
	); err != nil {
		return Session{}, Run{}, fmt.Errorf("insert session: %w", err)
	}
	run, err := insertRun(ctx, tx, session.ID, goal, nil)
	if err != nil {
		return Session{}, Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, Run{}, fmt.Errorf("commit create session: %w", err)
	}
	return session, run, nil
}

// GetSession 读取会话，并填上派生字段。
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(
		ctx, `SELECT id, goal, created_at, updated_at FROM sessions WHERE id = ?`, id,
	)
	var (
		session   Session
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&session.ID, &session.Goal, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("scan session: %w", err)
	}
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	if err := s.fillSessionDerived(ctx, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

// ListSessions 按创建时间倒序列出会话，含轮次数、最新一轮状态与累计用量。
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT s.id, s.goal, s.created_at, s.updated_at,
		        (SELECT COUNT(*) FROM runs r WHERE r.session_id = s.id),
		        COALESCE((SELECT r.status FROM runs r WHERE r.session_id = s.id
		                  ORDER BY r.created_at DESC, r.rowid DESC LIMIT 1), '')
		 FROM sessions s
		 ORDER BY s.created_at DESC, s.rowid DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]Session, 0, limit)
	for rows.Next() {
		var (
			session   Session
			createdAt string
			updatedAt string
		)
		if err := rows.Scan(
			&session.ID, &session.Goal, &createdAt, &updatedAt,
			&session.RunCount, &session.Status,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		session.CreatedAt = parseTime(createdAt)
		session.UpdatedAt = parseTime(updatedAt)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 一次查出所有会话的用量再回填，避免每个会话一次查询。
	usages, err := s.sessionUsages(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if value, ok := usages[sessions[i].ID]; ok {
			sessions[i].Usage = value
		}
	}
	return sessions, nil
}

// SessionUsage 汇总一个会话全部轮次的模型用量。
func (s *Store) SessionUsage(ctx context.Context, sessionID string) (usage.Usage, error) {
	var total usage.Usage
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(SUM(m.model_calls), 0),
		        COALESCE(SUM(m.prompt_tokens), 0),
		        COALESCE(SUM(m.completion_tokens), 0),
		        COALESCE(SUM(m.total_tokens), 0),
		        COALESCE(SUM(m.reasoning_tokens), 0),
		        COALESCE(SUM(m.cached_tokens), 0)
		 FROM model_usage m JOIN runs r ON r.id = m.run_id
		 WHERE r.session_id = ?`,
		sessionID,
	).Scan(
		&total.ModelCalls, &total.PromptTokens, &total.CompletionTokens,
		&total.TotalTokens, &total.ReasoningTokens, &total.CachedTokens,
	); err != nil {
		return usage.Usage{}, fmt.Errorf("session usage: %w", err)
	}
	return total.Normalize(), nil
}

// fillSessionDerived 填 RunCount / Status / Usage。
func (s *Store) fillSessionDerived(ctx context.Context, session *Session) error {
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT (SELECT COUNT(*) FROM runs r WHERE r.session_id = ?),
		        COALESCE((SELECT r.status FROM runs r WHERE r.session_id = ?
		                  ORDER BY r.created_at DESC, r.rowid DESC LIMIT 1), '')`,
		session.ID, session.ID,
	).Scan(&session.RunCount, &session.Status); err != nil {
		return fmt.Errorf("session derived fields: %w", err)
	}
	total, err := s.SessionUsage(ctx, session.ID)
	if err != nil {
		return err
	}
	session.Usage = total
	return nil
}

// sessionUsages 一次算出全部会话的累计用量，按 session_id 索引。
func (s *Store) sessionUsages(ctx context.Context) (map[string]usage.Usage, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.session_id,
		        COALESCE(SUM(m.model_calls), 0),
		        COALESCE(SUM(m.prompt_tokens), 0),
		        COALESCE(SUM(m.completion_tokens), 0),
		        COALESCE(SUM(m.total_tokens), 0),
		        COALESCE(SUM(m.reasoning_tokens), 0),
		        COALESCE(SUM(m.cached_tokens), 0)
		 FROM model_usage m JOIN runs r ON r.id = m.run_id
		 WHERE r.session_id IS NOT NULL
		 GROUP BY r.session_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("session usages: %w", err)
	}
	defer rows.Close()
	result := map[string]usage.Usage{}
	for rows.Next() {
		var (
			sessionID string
			value     usage.Usage
		)
		if err := rows.Scan(
			&sessionID, &value.ModelCalls, &value.PromptTokens, &value.CompletionTokens,
			&value.TotalTokens, &value.ReasoningTokens, &value.CachedTokens,
		); err != nil {
			return nil, fmt.Errorf("scan session usage: %w", err)
		}
		result[sessionID] = value.Normalize()
	}
	return result, rows.Err()
}
