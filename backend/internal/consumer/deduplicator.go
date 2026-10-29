// Package consumer provides transactionally deduplicated event handlers.
package consumer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tx is the PostgreSQL transaction that encloses an inbox insert and its handler's durable effects.
type Tx = pgx.Tx

// Handler persists the effect of one event in the supplied transaction.
type Handler func(context.Context, Tx, eventbus.Message) error

// Deduplicator prevents a named consumer from applying an event more than once.
type Deduplicator struct {
	pool     *pgxpool.Pool
	consumer string

	// ClaimTTL bounds recovery time for abandoned external handlers.
	// Zero uses the default of 30 seconds.
	ClaimTTL time.Duration
}

// NewDeduplicator creates an inbox-backed deduplicator for a consumer name.
func NewDeduplicator(pool *pgxpool.Pool, name string) *Deduplicator {
	return &Deduplicator{pool: pool, consumer: name}
}

// Handle runs handler only for the first successful delivery of an event ID.
func (d *Deduplicator) Handle(ctx context.Context, message eventbus.Message, handler Handler) error {
	if d == nil || d.pool == nil {
		return errors.New("inbox deduplicator is not configured")
	}
	if strings.TrimSpace(d.consumer) == "" {
		return errors.New("inbox consumer is required")
	}
	if handler == nil {
		return errors.New("inbox handler is required")
	}

	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		return fmt.Errorf("decode inbox event: %w", err)
	}
	if strings.TrimSpace(envelope.EventID) == "" {
		return errors.New("inbox event id is required")
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin inbox transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	inboxID, err := newInboxID()
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO inbox_events (id, consumer, event_id, payload)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (consumer, event_id) DO NOTHING`,
		inboxID, d.consumer, envelope.EventID, message.Value,
	)
	if err != nil {
		return fmt.Errorf("insert inbox event %q: %w", envelope.EventID, err)
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit duplicate inbox event %q: %w", envelope.EventID, err)
		}
		return nil
	}

	if err := handler(ctx, tx, message); err != nil {
		return fmt.Errorf("handle inbox event %q: %w", envelope.EventID, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE inbox_events
		SET processed_at = now()
		WHERE id = $1`, inboxID); err != nil {
		return fmt.Errorf("mark inbox event %q processed: %w", envelope.EventID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inbox event %q: %w", envelope.EventID, err)
	}
	return nil
}

func newInboxID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate inbox id: %w", err)
	}
	return "inb_" + hex.EncodeToString(bytes), nil
}
