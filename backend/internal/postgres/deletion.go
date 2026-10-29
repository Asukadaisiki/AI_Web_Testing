package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func deleteConversationData(ctx context.Context, tx pgx.Tx, conversationIDs []string) error {
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
	return nil
}

func (s *Store) DeleteConversationAs(ctx context.Context, conversationID, actor string) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	return s.deleteManagedConversation(ctx, conversationID, actor)
}

func (s *Store) deleteManagedConversation(ctx context.Context, conversationID, actor string) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := lockConversation(ctx, tx, conversationID); errors.Is(err, ErrNotFound) {
		return tx.Commit(ctx)
	} else if err != nil {
		return err
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM conversations WHERE id = $1`, conversationID).Scan(&state); err != nil {
		return err
	}
	if state == "deleting" || state == "deleted" {
		return tx.Commit(ctx)
	}
	environment, err := scanEnvironment(tx.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE conversation_id = $1 FOR UPDATE`, conversationID))
	if err != nil {
		return err
	}
	if err := managementConversationSafe(ctx, tx, environment, ""); err != nil {
		return err
	}
	if environment.State == "provisioning" || environment.State == "resetting" {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET state = 'deleting', updated_at = now() WHERE id = $1`, conversationID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `UPDATE environments SET state = 'deleting', fencing_token=fencing_token+1, updated_at = now() WHERE id = $1 RETURNING fencing_token`, environment.ID).Scan(&environment.FencingToken); err != nil {
		return err
	}
	environment.State = "deleting"
	if err := enqueueEnvironmentCommand(ctx, tx, environment, "delete", actor); err != nil {
		return err
	}
	if err := appendManagementAudit(ctx, tx, environment, actor, "conversation.delete_requested"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func deliverySafeForDeletion(ctx context.Context, tx pgx.Tx, runIDs []string) (bool, error) {
	var safe bool
	err := tx.QueryRow(ctx, `
		SELECT
		  NOT EXISTS (
		    SELECT 1
		    FROM outbox_events
		    WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id', key) = ANY($1)
		      AND published_at IS NULL
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM outbox_events AS command
		    WHERE COALESCE(command.payload->'payload'->>'run_id', command.payload->>'aggregate_id', command.key) = ANY($1)
		      AND command.topic = $2
		      AND NOT EXISTS (
		        SELECT 1
		        FROM inbox_events AS inbox
		        WHERE inbox.consumer = 'agenttrace-runtime'
		          AND inbox.event_id = command.event_id
		          AND inbox.processed_at IS NOT NULL
		      )
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM inbox_events
		    WHERE processed_at IS NULL AND (
		      COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id') = ANY($1)
		      OR event_id IN (
		        SELECT event_id FROM outbox_events
		        WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id', key) = ANY($1)
		      )
		    )
		  )`, runIDs, runCommandsTopic).Scan(&safe)
	if err != nil {
		return false, fmt.Errorf("check deletion delivery safety: %w", err)
	}
	return safe, nil
}

func deleteRunDeliveryRecords(ctx context.Context, tx pgx.Tx, runIDs []string) error {
	// RunCommand routing keys can be Environment IDs. Envelope identity owns
	// the content; keys are only a fallback for older run event envelopes.
	if _, err := tx.Exec(ctx, `
		DELETE FROM consumer_retry_counts
		WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id') = ANY($1)
		  OR event_id IN (
		    SELECT event_id FROM outbox_events
		    WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id', key) = ANY($1)
		)`, runIDs); err != nil {
		return fmt.Errorf("delete consumer retries: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE inbox_events
		SET payload = '{}'::jsonb
		WHERE processed_at IS NOT NULL AND (
		  COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id') = ANY($1)
		  OR event_id IN (
		    SELECT event_id FROM outbox_events
		    WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id', key) = ANY($1)
		  )
		)`, runIDs); err != nil {
		return fmt.Errorf("scrub retained run inbox payloads: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM outbox_events
		WHERE COALESCE(payload->'payload'->>'run_id', payload->>'aggregate_id', key) = ANY($1)`, runIDs); err != nil {
		return fmt.Errorf("delete outbox records: %w", err)
	}
	return nil
}

func deleteEnvironmentDeliveryRecords(ctx context.Context, tx pgx.Tx, environment Environment) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM consumer_retry_counts
		WHERE payload->'payload'->>'conversation_id' = $1
		  OR payload->'payload'->>'environment_id' = $2
		  OR event_id IN (SELECT event_id FROM environment_commands WHERE environment_id = $2)
		  OR event_id IN (
		    SELECT event_id FROM outbox_events
		    WHERE payload->'payload'->>'conversation_id' = $1
		      OR payload->'payload'->>'environment_id' = $2
		  )`, environment.ConversationID, environment.ID); err != nil {
		return fmt.Errorf("delete environment consumer retries: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE inbox_events SET payload = '{}'::jsonb
		WHERE processed_at IS NOT NULL AND (
		  payload->'payload'->>'conversation_id' = $1
		  OR payload->'payload'->>'environment_id' = $2
		  OR event_id IN (SELECT event_id FROM environment_commands WHERE environment_id = $2)
		  OR event_id IN (
		    SELECT event_id FROM outbox_events
		    WHERE payload->'payload'->>'conversation_id' = $1
		      OR payload->'payload'->>'environment_id' = $2
		  )
		)`, environment.ConversationID, environment.ID); err != nil {
		return fmt.Errorf("scrub retained environment inbox payloads: %w", err)
	}
	// Completion proves delivery, even if the publisher has not yet recorded
	// its acknowledgement. Command hashes/tokens and scrubbed inbox identities
	// suffice for replay; retaining mutated envelopes would break hash checks.
	if _, err := tx.Exec(ctx, `
		DELETE FROM outbox_events
		WHERE payload->'payload'->>'conversation_id' = $1
		  OR payload->'payload'->>'environment_id' = $2
		  OR event_id IN (SELECT event_id FROM environment_commands WHERE environment_id = $2)`,
		environment.ConversationID, environment.ID); err != nil {
		return fmt.Errorf("delete environment outbox records: %w", err)
	}
	return nil
}
