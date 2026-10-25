package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DeleteProject removes a fully delivered project and remains successful when
// the project is already absent.
func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete project: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockProject(ctx, tx, projectID); errors.Is(err, ErrNotFound) {
		return tx.Commit(ctx)
	} else if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT id
		FROM conversations
		WHERE project_id = $1
		ORDER BY id
		FOR UPDATE`, projectID)
	if err != nil {
		return fmt.Errorf("lock project conversations: %w", err)
	}
	var conversationIDs []string
	for rows.Next() {
		var conversationID string
		if err := rows.Scan(&conversationID); err != nil {
			rows.Close()
			return fmt.Errorf("scan project conversation: %w", err)
		}
		conversationIDs = append(conversationIDs, conversationID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate project conversations: %w", err)
	}
	rows.Close()

	if err := deleteConversations(ctx, tx, conversationIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete project: %w", err)
	}
	return nil
}

// DeleteConversation removes a fully delivered conversation and remains
// successful when the conversation is already absent.
func (s *Store) DeleteConversation(ctx context.Context, conversationID string) error {
	if s == nil || s.pool == nil {
		return errors.New("postgres store is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete conversation: %w", err)
	}
	defer tx.Rollback(ctx)

	var projectID string
	err = tx.QueryRow(ctx, `
		SELECT project_id
		FROM conversations
		WHERE id = $1`, conversationID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("read conversation project: %w", err)
	}
	if err := lockProject(ctx, tx, projectID); errors.Is(err, ErrNotFound) {
		return tx.Commit(ctx)
	} else if err != nil {
		return err
	}
	var lockedID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM conversations
		WHERE id = $1 AND project_id = $2
		FOR UPDATE`, conversationID, projectID).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("lock conversation for deletion: %w", err)
	}

	if err := deleteConversations(ctx, tx, []string{conversationID}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete conversation: %w", err)
	}
	return nil
}

func deleteConversations(ctx context.Context, tx pgx.Tx, conversationIDs []string) error {
	if len(conversationIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id, status
		FROM runs
		WHERE conversation_id = ANY($1)
		ORDER BY id
		FOR UPDATE`, conversationIDs)
	if err != nil {
		return fmt.Errorf("lock deletion runs: %w", err)
	}
	var runIDs []string
	for rows.Next() {
		var runID, status string
		if err := rows.Scan(&runID, &status); err != nil {
			rows.Close()
			return fmt.Errorf("scan deletion run: %w", err)
		}
		if status != "succeeded" && status != "failed" && status != "cancelled" {
			rows.Close()
			return ErrConflict
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate deletion runs: %w", err)
	}
	rows.Close()

	if len(runIDs) > 0 {
		safe, err := deliverySafeForDeletion(ctx, tx, runIDs)
		if err != nil {
			return err
		}
		if !safe {
			return ErrConflict
		}
		if err := deleteRunDeliveryRecords(ctx, tx, runIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM idempotency_keys
			WHERE response ->> 'run_id' = ANY($1)
			   OR scope = ANY(
			     SELECT 'run-response:' || run_id
			     FROM unnest($1::text[]) AS run_id
			   )`, runIDs); err != nil {
			return fmt.Errorf("delete run idempotency records: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM runs WHERE id = ANY($1)`, runIDs); err != nil {
			return fmt.Errorf("delete runs: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_keys
		WHERE scope = ANY(
		  SELECT 'conversation-message:' || conversation_id
		  FROM unnest($1::text[]) AS conversation_id
		)`, conversationIDs); err != nil {
		return fmt.Errorf("delete conversation idempotency records: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM conversations WHERE id = ANY($1)`, conversationIDs); err != nil {
		return fmt.Errorf("delete conversations: %w", err)
	}
	return nil
}

func deliverySafeForDeletion(ctx context.Context, tx pgx.Tx, runIDs []string) (bool, error) {
	var safe bool
	err := tx.QueryRow(ctx, `
		SELECT
		  NOT EXISTS (
		    SELECT 1
		    FROM outbox_events
		    WHERE key = ANY($1) AND published_at IS NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM outbox_events AS command
		    WHERE command.key = ANY($1)
		      AND command.topic = $2
		      AND NOT EXISTS (
		        SELECT 1
		        FROM inbox_events AS inbox
		        WHERE inbox.consumer = 'agenttrace-runtime'
		          AND inbox.event_id = command.event_id
		          AND inbox.processed_at IS NOT NULL
		      )
		  )`, runIDs, runCommandsTopic).Scan(&safe)
	if err != nil {
		return false, fmt.Errorf("check deletion delivery safety: %w", err)
	}
	return safe, nil
}

func deleteRunDeliveryRecords(ctx context.Context, tx pgx.Tx, runIDs []string) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM consumer_retry_counts
		WHERE event_id IN (
		  SELECT event_id FROM outbox_events WHERE key = ANY($1)
		)`, runIDs); err != nil {
		return fmt.Errorf("delete consumer retries: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE inbox_events AS inbox
		SET payload = '{}'::jsonb
		FROM outbox_events AS event
		WHERE inbox.event_id = event.event_id
		  AND event.key = ANY($1)
		  AND inbox.consumer = 'agenttrace-runtime'
		  AND inbox.processed_at IS NOT NULL
		  AND event.topic = $2`, runIDs, runCommandsTopic); err != nil {
		return fmt.Errorf("scrub retained runtime inbox payloads: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM inbox_events AS inbox
		USING outbox_events AS event
		WHERE inbox.event_id = event.event_id
		  AND event.key = ANY($1)
		  AND NOT (
		    inbox.consumer = 'agenttrace-runtime'
		    AND inbox.processed_at IS NOT NULL
		    AND event.topic = $2
		  )`, runIDs, runCommandsTopic); err != nil {
		return fmt.Errorf("delete disposable inbox records: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM outbox_events WHERE key = ANY($1)`, runIDs); err != nil {
		return fmt.Errorf("delete outbox records: %w", err)
	}
	return nil
}
