package postgres

import (
	"context"
	"strings"
)

// ReviewEnvironment records an explicit user decision after uncertain external
// effects. It never retries a Run and requires all delivery and leases drained.
func (s *Store) ReviewEnvironment(ctx context.Context, id, actor string) (Environment, error) {
	if err := s.requireManagement(); err != nil {
		return Environment{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return Environment{}, ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Environment{}, err
	}
	defer tx.Rollback(ctx)
	env, err := lockEnvironment(ctx, tx, id)
	if err != nil {
		return Environment{}, err
	}
	if env.State != "needs_review" || env.ProfileRef == "" {
		return Environment{}, ErrConflict
	}
	if err := managementConversationSafe(ctx, tx, env, ""); err != nil {
		return Environment{}, err
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM control_sessions WHERE environment_id=$1
		AND profile_generation=$2 AND (state IN ('pending','opening','leased','busy','closing')
		OR state='needs_review'))`, env.ID, env.ProfileGeneration).Scan(&blocked); err != nil {
		return Environment{}, err
	}
	if blocked {
		return Environment{}, ErrConflict
	}
	env, err = scanEnvironment(tx.QueryRow(ctx, `UPDATE environments SET state='ready',
		fencing_token=fencing_token+1, updated_at=now() WHERE id=$1 RETURNING `+environmentColumns, id))
	if err != nil {
		return Environment{}, err
	}
	if err := appendManagementAudit(ctx, tx, env, actor, "environment.reviewed"); err != nil {
		return Environment{}, err
	}
	return env, tx.Commit(ctx)
}
