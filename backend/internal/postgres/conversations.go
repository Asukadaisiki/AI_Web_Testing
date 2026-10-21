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
		WHERE id = $1
		FOR UPDATE`, conversationID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("conversation %q: %w", conversationID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("lock conversation %q: %w", conversationID, err)
	}
	if err := lockProject(ctx, tx, projectID); err != nil {
		return "", err
	}
	return projectID, nil
}
