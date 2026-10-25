// Package postgres provides PostgreSQL-backed durable AgentTrace repositories.
package postgres

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrConflict reports an idempotency key or durable effect replay with different content.
	ErrConflict = errors.New("durable payload conflict")
	ErrNotFound = errors.New("durable record not found")
)

// Store owns transaction-scoped access to the AgentTrace PostgreSQL ledger.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore binds repositories to an existing PostgreSQL connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func newID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func payloadHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal payload for hash: %w", err)
	}

	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return "", fmt.Errorf("normalize payload for hash: %w", err)
	}
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal normalized payload for hash: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return raw
}

func requiredJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

type rowScanner interface {
	Scan(...any) error
}

func scanRun(row rowScanner) (domain.Run, error) {
	var run domain.Run
	err := row.Scan(
		&run.ID,
		&run.ProjectID,
		&run.ConversationID,
		&run.SourceMessageID,
		&run.RetryOfRunID,
		&run.Status,
		&run.Specification,
		&run.CheckpointVersion,
		&run.CancelRequestedAt,
		&run.StartedAt,
		&run.FinishedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, ErrNotFound
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("scan run: %w", err)
	}
	return run, nil
}

func scanMessage(row rowScanner) (domain.Message, error) {
	var message domain.Message
	err := row.Scan(
		&message.ID,
		&message.ConversationID,
		&message.RunID,
		&message.Ordinal,
		&message.Role,
		&message.Content,
		&message.Metadata,
		&message.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, ErrNotFound
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("scan message: %w", err)
	}
	return message, nil
}
