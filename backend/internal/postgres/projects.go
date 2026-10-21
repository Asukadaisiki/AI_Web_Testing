package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func lockProject(ctx context.Context, tx pgx.Tx, projectID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("project %q: %w", projectID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lock project %q: %w", projectID, err)
	}
	return nil
}
