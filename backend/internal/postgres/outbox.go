package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	runCommandsTopic = "agenttrace.run.commands.v1"
	runEventsTopic   = "agenttrace.run.events.v1"
)

func insertOutbox(
	ctx context.Context,
	tx pgx.Tx,
	eventID, topic, key string,
	payload any,
	payloadHash string,
) (bool, error) {
	id, err := newID("obx")
	if err != nil {
		return false, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal outbox event %q: %w", eventID, err)
	}
	headers, err := json.Marshal(map[string]string{"payload_hash": payloadHash})
	if err != nil {
		return false, fmt.Errorf("marshal outbox headers: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, event_id, topic, key, payload, headers)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb)
		ON CONFLICT (event_id) DO NOTHING`,
		id, eventID, topic, key, encoded, headers,
	)
	if err != nil {
		return false, fmt.Errorf("insert outbox event %q: %w", eventID, err)
	}
	return tag.RowsAffected() == 1, nil
}

func outboxPayloadHash(ctx context.Context, tx pgx.Tx, eventID string) (string, error) {
	var hash string
	err := tx.QueryRow(ctx, `
		SELECT headers ->> 'payload_hash'
		FROM outbox_events
		WHERE event_id = $1
		FOR UPDATE`, eventID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read outbox event %q: %w", eventID, err)
	}
	return hash, nil
}
