package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// AddUsage 把一次模型调用的用量累加到该 run 上，返回累加后的总量。
//
// 增量累加（而不是在内存里攒完再写）有两个好处：进程中途挂掉不丢已经花掉的钱；
// 长 run 进行中就能读到当前成本。返回值就是新的总量，调用方不必自己再维护一份。
func (s *Store) AddUsage(ctx context.Context, runID string, delta usage.Usage) (usage.Usage, error) {
	if runID == "" {
		return usage.Usage{}, errors.New("run id is required")
	}
	delta = delta.Normalize()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO model_usage (
  run_id, model_calls, prompt_tokens, completion_tokens,
  total_tokens, reasoning_tokens, cached_tokens, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
  model_calls       = model_calls + excluded.model_calls,
  prompt_tokens     = prompt_tokens + excluded.prompt_tokens,
  completion_tokens = completion_tokens + excluded.completion_tokens,
  total_tokens      = total_tokens + excluded.total_tokens,
  reasoning_tokens  = reasoning_tokens + excluded.reasoning_tokens,
  cached_tokens     = cached_tokens + excluded.cached_tokens,
  updated_at        = excluded.updated_at`,
		runID,
		delta.ModelCalls, delta.PromptTokens, delta.CompletionTokens,
		delta.TotalTokens, delta.ReasoningTokens, delta.CachedTokens,
		formatTime(time.Now().UTC()),
	)
	if err != nil {
		return usage.Usage{}, fmt.Errorf("add model usage: %w", err)
	}
	return s.GetUsage(ctx, runID)
}

// GetUsage 读取某个 run 的累计用量；没有记录时返回零值（不是错误）。
func (s *Store) GetUsage(ctx context.Context, runID string) (usage.Usage, error) {
	var value usage.Usage
	err := s.db.QueryRowContext(ctx, `
SELECT model_calls, prompt_tokens, completion_tokens,
       total_tokens, reasoning_tokens, cached_tokens
FROM model_usage WHERE run_id = ?`, runID).Scan(
		&value.ModelCalls, &value.PromptTokens, &value.CompletionTokens,
		&value.TotalTokens, &value.ReasoningTokens, &value.CachedTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return usage.Usage{}, nil
	}
	if err != nil {
		return usage.Usage{}, fmt.Errorf("get model usage: %w", err)
	}
	return value.Normalize(), nil
}
