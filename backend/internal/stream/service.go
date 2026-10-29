// Package stream replays durable run events and tails disposable live updates.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPollInterval = time.Second

var ErrSubscriptionLimit = errors.New("stream subscription capacity reached")
var ErrSlowConsumer = errors.New("stream consumer exceeded delivery timeout")

// DurableEvent is a PostgreSQL-backed event that can be replayed with an SSE ID.
type DurableEvent struct {
	Sequence int64
	Type     string
	Data     json.RawMessage
}

// DurableEventReader returns durable events strictly after a sequence.
type DurableEventReader interface {
	ReadAfter(ctx context.Context, runID string, afterSequence int64) ([]DurableEvent, error)
}

// StreamEvent is an authoritative event ready for SSE encoding.
type StreamEvent struct {
	ID   string
	Type string
	Data json.RawMessage
}

// Config controls bounded live reads and PostgreSQL fallback polling.
type Config struct {
	ReadBlock        time.Duration
	PollInterval     time.Duration
	MaxSubscriptions int
	SendTimeout      time.Duration
}

// Service replays durable events, using the live store only to wake durable reads.
type Service struct {
	durable      DurableEventReader
	live         live.LiveStore
	config       Config
	slots        chan struct{}
	active       atomic.Int64
	rejected     atomic.Int64
	fallback     atomic.Int64
	durableReads atomic.Int64
	slow         atomic.Int64
}

type Stats struct {
	Active        int64 `json:"active_connections"`
	Rejected      int64 `json:"rejected_connections"`
	FallbackReads int64 `json:"redis_fallback_reads"`
	DurableReads  int64 `json:"durable_reads"`
	SlowConsumers int64 `json:"slow_consumers"`
}

func (s *Service) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	return Stats{s.active.Load(), s.rejected.Load(), s.fallback.Load(), s.durableReads.Load(), s.slow.Load()}
}

// NewService creates a stream service with one-second PostgreSQL polling by default.
func NewService(durable DurableEventReader, liveStore live.LiveStore, config Config) *Service {
	if config.ReadBlock <= 0 {
		config.ReadBlock = defaultPollInterval
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultPollInterval
	}
	if config.ReadBlock > config.PollInterval {
		config.ReadBlock = config.PollInterval
	}
	if config.MaxSubscriptions <= 0 {
		config.MaxSubscriptions = 256
	}
	if config.SendTimeout <= 0 {
		config.SendTimeout = 5 * time.Second
	}
	return &Service{durable: durable, live: liveStore, config: config, slots: make(chan struct{}, config.MaxSubscriptions)}
}

// Subscribe emits durable events after lastEventID until canceled.
func (s *Service) Subscribe(
	ctx context.Context, runID, lastEventID string,
) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent)
	streamErrs := make(chan error, 1)
	if s != nil {
		select {
		case s.slots <- struct{}{}:
			s.active.Add(1)
		default:
			s.rejected.Add(1)
			streamErrs <- ErrSubscriptionLimit
			close(events)
			close(streamErrs)
			return events, streamErrs
		}
	}

	go func() {
		defer close(events)
		defer close(streamErrs)
		if s != nil {
			defer func() { <-s.slots; s.active.Add(-1) }()
		}

		afterSequence, err := parseSequence(lastEventID)
		if err != nil {
			sendStreamError(ctx, streamErrs, err)
			return
		}
		if s == nil || s.durable == nil {
			sendStreamError(ctx, streamErrs, errors.New("stream service is not configured"))
			return
		}

		emitDurable := func() error {
			s.durableReads.Add(1)
			durableEvents, readErr := s.durable.ReadAfter(ctx, runID, afterSequence)
			if readErr != nil {
				return fmt.Errorf("read durable run events: %w", readErr)
			}
			for _, event := range durableEvents {
				if event.Sequence <= afterSequence {
					continue
				}
				deliveryCtx, cancel := context.WithTimeout(ctx, s.config.SendTimeout)
				err := sendStreamEvent(deliveryCtx, events, StreamEvent{
					ID:   strconv.FormatInt(event.Sequence, 10),
					Type: event.Type,
					Data: event.Data,
				})
				cancel()
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) {
						s.slow.Add(1)
						return ErrSlowConsumer
					}
					return err
				}
				afterSequence = event.Sequence
			}
			return nil
		}
		if err := emitDurable(); err != nil {
			if !errors.Is(err, context.Canceled) {
				sendStreamError(ctx, streamErrs, err)
			}
			return
		}

		afterLiveID := "0-0"
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte(runID))
		pollInterval := s.config.PollInterval + time.Duration(hasher.Sum32()%251)*s.config.PollInterval/1000
		for {
			if s.live == nil {
				s.fallback.Add(1)
				if err := emitDurable(); err != nil {
					if !errors.Is(err, context.Canceled) {
						sendStreamError(ctx, streamErrs, err)
					}
					return
				}
				if !wait(ctx, pollInterval) {
					return
				}
				continue
			}
			liveEvents, readErr := s.live.Read(ctx, runID, afterLiveID, s.config.ReadBlock)
			if readErr != nil {
				if ctx.Err() != nil {
					return
				}
				s.fallback.Add(1)
				if err := emitDurable(); err != nil {
					if !errors.Is(err, context.Canceled) {
						sendStreamError(ctx, streamErrs, err)
					}
					return
				}
				if !wait(ctx, pollInterval) {
					return
				}
				continue
			}

			for _, event := range liveEvents {
				if event.ID != "" {
					afterLiveID = event.ID
				}
			}
			if err := emitDurable(); err != nil {
				if !errors.Is(err, context.Canceled) {
					sendStreamError(ctx, streamErrs, err)
				}
				return
			}
		}
	}()

	return events, streamErrs
}

func parseSequence(lastEventID string) (int64, error) {
	if lastEventID == "" {
		return 0, nil
	}
	sequence, err := strconv.ParseInt(lastEventID, 10, 64)
	if err != nil || sequence < 0 {
		return 0, fmt.Errorf("invalid last event id %q", lastEventID)
	}
	return sequence, nil
}

func sendStreamEvent(ctx context.Context, events chan<- StreamEvent, event StreamEvent) error {
	select {
	case events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sendStreamError(ctx context.Context, streamErrs chan<- error, err error) {
	select {
	case streamErrs <- err:
	case <-ctx.Done():
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// PostgreSQLReader reads authoritative run_events in sequence order.
type PostgreSQLReader struct {
	pool *pgxpool.Pool
}

// NewPostgreSQLReader binds a durable reader to a PostgreSQL pool.
func NewPostgreSQLReader(pool *pgxpool.Pool) *PostgreSQLReader {
	return &PostgreSQLReader{pool: pool}
}

// ReadAfter returns run events whose sequence is greater than afterSequence.
func (r *PostgreSQLReader) ReadAfter(
	ctx context.Context, runID string, afterSequence int64,
) ([]DurableEvent, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres event reader is not configured")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT seq, event_type, payload
		FROM run_events
		WHERE run_id = $1 AND seq > $2
		ORDER BY seq ASC LIMIT 256`, runID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []DurableEvent
	for rows.Next() {
		var event DurableEvent
		if err := rows.Scan(&event.Sequence, &event.Type, &event.Data); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
