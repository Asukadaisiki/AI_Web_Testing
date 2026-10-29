package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ArchiveConversation(ctx context.Context, conversationID, actor string) (Conversation, error) {
	if err := s.requireManagement(); err != nil {
		return Conversation{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Conversation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockConversation(ctx, tx, conversationID); err != nil {
		return Conversation{}, err
	}
	conversation, err := scanConversation(tx.QueryRow(ctx, `SELECT id, project_id, COALESCE(title, ''), metadata, created_at, updated_at, state, deleted_at FROM conversations WHERE id = $1`, conversationID))
	if err != nil {
		return Conversation{}, err
	}
	if conversation.State == "archived" {
		return conversation, tx.Commit(ctx)
	}
	if conversation.State != "active" {
		return Conversation{}, ErrConflict
	}
	conversation, err = scanConversation(tx.QueryRow(ctx, `UPDATE conversations SET state = 'archived', updated_at = now() WHERE id = $1 RETURNING id, project_id, COALESCE(title, ''), metadata, created_at, updated_at, state, deleted_at`, conversationID))
	if err != nil {
		return Conversation{}, err
	}
	if err := appendManagementAudit(ctx, tx, Environment{ProjectID: conversation.ProjectID, ConversationID: conversation.ID}, actor, "conversation.archived"); err != nil {
		return Conversation{}, err
	}
	return conversation, tx.Commit(ctx)
}

// CheckConversationRunnable is a preflight for callers outside this package.
// Run creation must call checkConversationRunnable in its existing transaction
// after lockConversation, not rely on this preflight to avoid a reset/delete race.
func (s *Store) CheckConversationRunnable(ctx context.Context, conversationID string) (Environment, error) {
	if err := s.requireManagement(); err != nil {
		return Environment{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Environment{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockConversation(ctx, tx, conversationID); err != nil {
		return Environment{}, err
	}
	environment, err := s.checkConversationRunnable(ctx, tx, conversationID)
	if err != nil {
		return Environment{}, err
	}
	return environment, tx.Commit(ctx)
}

func (s *Store) checkConversationRunnable(ctx context.Context, tx pgx.Tx, conversationID string) (Environment, error) {
	return lockReadyEnvironmentForRun(ctx, tx, conversationID)
}

// Caller must hold lockConversation before this helper; idempotent Run replay
// must be resolved first. Use the returned generation in an explicit rerun.
func lockReadyEnvironmentForRun(ctx context.Context, tx pgx.Tx, conversationID string) (Environment, error) {
	var state, bindingStatus string
	err := tx.QueryRow(ctx, `SELECT c.state, b.status FROM conversations c JOIN project_bindings b ON b.agenttrace_project_id = c.project_id WHERE c.id = $1`, conversationID).Scan(&state, &bindingStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrConflict
	}
	if err != nil {
		return Environment{}, err
	}
	if state != "active" || bindingStatus != "active" {
		return Environment{}, ErrConflict
	}
	environment, err := scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE conversation_id = $1 FOR UPDATE`, conversationID))
	if err != nil {
		return Environment{}, err
	}
	if environment.State != "ready" {
		return Environment{}, ErrConflict
	}
	return environment, nil
}

func lockConversation(ctx context.Context, tx pgx.Tx, conversationID string) (string, error) {
	var projectID string
	err := tx.QueryRow(ctx, `
		SELECT project_id
		FROM conversations
		WHERE id = $1`, conversationID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("conversation %q: %w", conversationID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("read conversation project %q: %w", conversationID, err)
	}
	if err := lockProject(ctx, tx, projectID); err != nil {
		return "", err
	}
	var lockedProjectID string
	err = tx.QueryRow(ctx, `
		SELECT project_id
		FROM conversations
		WHERE id = $1 AND project_id = $2
		FOR UPDATE`, conversationID, projectID).Scan(&lockedProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("conversation %q: %w", conversationID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("lock conversation %q: %w", conversationID, err)
	}
	return projectID, nil
}
