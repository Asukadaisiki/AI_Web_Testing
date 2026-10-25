package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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
