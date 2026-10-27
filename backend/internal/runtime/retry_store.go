package runtime

import (
	"context"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// PostgresRetryStore persists consumer failures across Consumer instances.
type PostgresRetryStore struct {
	Store    *postgres.Store
	Consumer string
}

// Increment atomically increments the durable delivery count.
func (s PostgresRetryStore) Increment(ctx context.Context, eventID string, payload []byte) (RetryIncrement, error) {
	increment, err := s.Store.IncrementConsumerRetryUnlessDLQHandoff(
		ctx,
		s.Consumer,
		eventID,
		stableID("consumer-dlq", eventID),
		payload,
	)
	if err != nil {
		return RetryIncrement{}, err
	}
	return RetryIncrement{
		Attempts:     increment.Attempts,
		DLQHandedOff: increment.DLQHandedOff,
	}, nil
}

// Clear removes a completed delivery's retry state.
func (s PostgresRetryStore) Clear(ctx context.Context, eventID string) error {
	return s.Store.ClearConsumerRetry(ctx, s.Consumer, eventID)
}

// HandoffDLQ atomically enqueues the DLQ record and removes retry state.
func (s PostgresRetryStore) HandoffDLQ(ctx context.Context, eventID string, handoff DLQHandoff) error {
	return s.Store.HandoffConsumerRetryToOutbox(
		ctx,
		s.Consumer,
		eventID,
		handoff.EventID,
		handoff.Topic,
		handoff.Key,
		handoff.Payload,
	)
}

// HasDLQHandoff reports whether this source record was already handed to the
// durable outbox during a previous delivery.
func (s PostgresRetryStore) HasDLQHandoff(ctx context.Context, _ string, outboxEventID string) (bool, error) {
	return s.Store.HasConsumerRetryDLQHandoff(ctx, outboxEventID)
}
