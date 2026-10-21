// Package outbox relays durable PostgreSQL outbox records to an EventBus.
package outbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultBatchSize      = 100
	defaultLeaseDuration  = time.Minute
	defaultInitialBackoff = time.Second
	maxRetryBackoff       = 5 * time.Minute
	maxErrorLength        = 1024
)

// Config controls how a Relay claims and retries outbox records.
type Config struct {
	WorkerID       string
	BatchSize      int
	LeaseDuration  time.Duration
	InitialBackoff time.Duration
}

// Relay claims outbox records in short transactions and publishes them outside PostgreSQL.
type Relay struct {
	pool   *pgxpool.Pool
	bus    eventbus.EventBus
	config Config
}

// NewRelay creates a relay with conservative defaults for omitted timing settings.
func NewRelay(pool *pgxpool.Pool, bus eventbus.EventBus, config Config) *Relay {
	if config.BatchSize < 1 {
		config.BatchSize = defaultBatchSize
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = defaultInitialBackoff
	}
	return &Relay{pool: pool, bus: bus, config: config}
}

// RelayOnce claims one bounded batch and returns the number acknowledged by the broker.
func (r *Relay) RelayOnce(ctx context.Context) (int, error) {
	if r == nil || r.pool == nil || r.bus == nil {
		return 0, errors.New("outbox relay is not configured")
	}
	if strings.TrimSpace(r.config.WorkerID) == "" {
		return 0, errors.New("outbox relay worker id is required")
	}

	records, err := r.claim(ctx)
	if err != nil {
		return 0, err
	}

	var (
		published int
		firstErr  error
	)
	for _, record := range records {
		metadata, err := r.bus.Publish(ctx, record.topic, record.key, record.payload)
		if err != nil {
			if failureErr := r.recordFailure(ctx, record.id, record.claimToken, err); failureErr != nil {
				err = fmt.Errorf("publish outbox event %q: %w", record.eventID, errors.Join(err, failureErr))
			} else {
				err = fmt.Errorf("publish outbox event %q: %w", record.eventID, err)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		acknowledged, err := r.recordPublished(ctx, record.id, record.claimToken, metadata)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if acknowledged {
			published++
		}
	}
	return published, firstErr
}

type claimedRecord struct {
	id         string
	eventID    string
	topic      string
	key        string
	payload    []byte
	createdAt  time.Time
	claimToken string
}

func (r *Relay) claim(ctx context.Context) ([]claimedRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer tx.Rollback(ctx)

	leaseUntil := time.Now().UTC().Add(r.config.LeaseDuration)
	claimToken, err := newClaimToken()
	if err != nil {
		return nil, fmt.Errorf("generate outbox claim token: %w", err)
	}
	rows, err := tx.Query(ctx, `
		WITH candidates AS (
			SELECT event.id
			FROM outbox_events AS event
			WHERE event.published_at IS NULL
			  AND event.next_attempt_at <= now()
			  AND (event.locked_until IS NULL OR event.locked_until <= now())
			  AND NOT EXISTS (
				SELECT 1
				FROM outbox_events AS earlier
				WHERE earlier.published_at IS NULL
				  AND earlier.topic = event.topic
				  AND earlier.key = event.key
				  AND (earlier.created_at, earlier.id) < (event.created_at, event.id)
			  )
			ORDER BY event.created_at, event.id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events AS event
		SET locked_by = $2, locked_until = $3, claim_token = $4
		FROM candidates
		WHERE event.id = candidates.id
		RETURNING event.id, event.event_id, event.topic, event.key, event.payload, event.created_at, event.claim_token`,
		r.config.BatchSize, r.config.WorkerID, leaseUntil, claimToken,
	)
	if err != nil {
		return nil, fmt.Errorf("claim outbox batch: %w", err)
	}
	defer rows.Close()

	var records []claimedRecord
	for rows.Next() {
		var record claimedRecord
		if err := rows.Scan(
			&record.id,
			&record.eventID,
			&record.topic,
			&record.key,
			&record.payload,
			&record.createdAt,
			&record.claimToken,
		); err != nil {
			return nil, fmt.Errorf("scan claimed outbox event: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed outbox events: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].createdAt.Equal(records[j].createdAt) {
			return records[i].id < records[j].id
		}
		return records[i].createdAt.Before(records[j].createdAt)
	})
	return records, nil
}

func (r *Relay) recordPublished(ctx context.Context, id, claimToken string, metadata eventbus.Metadata) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = now(),
		    broker_partition = $2,
		    broker_offset = $3,
		    locked_by = NULL,
		    locked_until = NULL,
		    claim_token = NULL,
		    last_error = NULL
		WHERE id = $1
		  AND published_at IS NULL
		  AND claim_token = $4`,
		id, metadata.Partition, metadata.Offset, claimToken,
	)
	if err != nil {
		return false, fmt.Errorf("mark outbox event %q published: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Relay) recordFailure(ctx context.Context, id, claimToken string, publishErr error) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET attempt_count = attempt_count + 1,
		    next_attempt_at = now() + (
				LEAST(
					$2::bigint * (1::bigint << LEAST(attempt_count, 20)),
					$3::bigint
				) * interval '1 microsecond'
			),
		    locked_by = NULL,
		    locked_until = NULL,
		    claim_token = NULL,
		    last_error = $4
		WHERE id = $1
		  AND published_at IS NULL
		  AND claim_token = $5`,
		id,
		r.config.InitialBackoff.Microseconds(),
		maxRetryBackoff.Microseconds(),
		boundedError(publishErr),
		claimToken,
	)
	if err != nil {
		return fmt.Errorf("record outbox event %q publish failure: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func newClaimToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func boundedError(err error) string {
	value := err.Error()
	if len(value) <= maxErrorLength {
		return value
	}
	return value[:maxErrorLength]
}
