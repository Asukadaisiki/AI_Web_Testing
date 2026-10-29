package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrClaimInProgress keeps active duplicates unacknowledged without treating
	// normal claim contention as a failed execution.
	ErrClaimInProgress = errors.New("inbox event is already claimed")
	// ErrClaimLost prevents an old delivery from finalizing a replacement claim.
	ErrClaimLost = errors.New("inbox claim ownership lost")
)

// DeliveryDeferred marks temporary queue contention, not a failed execution.
// Schedulers may implement this interface or wrap their errors with DeferDelivery.
type DeliveryDeferred interface {
	error
	DeliveryDeferred() bool
}

// DeferDelivery preserves the cause while excluding contention from retry/DLQ budgets.
func DeferDelivery(cause error) error {
	if cause == nil {
		return nil
	}
	return &deferredDelivery{cause: cause}
}

type deferredDelivery struct{ cause error }

func (e *deferredDelivery) Error() string          { return e.cause.Error() }
func (e *deferredDelivery) Unwrap() error          { return e.cause }
func (e *deferredDelivery) DeliveryDeferred() bool { return true }

// IsDeliveryDeferred recognizes wrapped scheduler contention and inbox ownership errors.
func IsDeliveryDeferred(err error) bool {
	if errors.Is(err, ErrClaimInProgress) || errors.Is(err, ErrClaimLost) {
		return true
	}
	var deferred DeliveryDeferred
	return errors.As(err, &deferred) && deferred.DeliveryDeferred()
}

// HandleExternal commits a renewable claim before invoking handler, then marks
// the inbox processed in a separate short statement. No transaction or database
// connection is held across handler. Unlike Handle, handler effects are not atomic
// with inbox completion: it must reconcile durable attempts on redelivery rather
// than blindly repeating external effects.
func (d *Deduplicator) HandleExternal(
	ctx context.Context, message eventbus.Message, handler func(context.Context) error,
) error {
	if d == nil || d.pool == nil {
		return errors.New("inbox deduplicator is not configured")
	}
	if strings.TrimSpace(d.consumer) == "" {
		return errors.New("inbox consumer is required")
	}
	if handler == nil {
		return errors.New("inbox handler is required")
	}
	ttl := d.ClaimTTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	if ttl < 3*time.Millisecond {
		return errors.New("inbox claim TTL must be at least 3 milliseconds")
	}
	var envelope event.Envelope[json.RawMessage]
	if err := json.Unmarshal(message.Value, &envelope); err != nil {
		return fmt.Errorf("decode inbox event: %w", err)
	}
	if strings.TrimSpace(envelope.EventID) == "" {
		return errors.New("inbox event id is required")
	}
	token, err := newInboxID()
	if err != nil {
		return err
	}
	id, err := d.claimExternal(ctx, envelope.EventID, message.Value, token, ttl)
	if err != nil || id == "" {
		return err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	stop := make(chan struct{})
	renewed := make(chan error, 1)
	go func() {
		err := d.renewExternal(runCtx, stop, id, token, ttl)
		if err != nil {
			cancelRun()
		}
		renewed <- err
	}()
	var runErr, renewErr error
	func() {
		defer func() {
			close(stop)
			cancelRun()
			renewErr = <-renewed
		}()
		runErr = handler(runCtx)
	}()
	if renewErr != nil {
		return errors.Join(runErr, renewErr)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if runErr != nil {
		return errors.Join(runErr, d.finishExternal(finishCtx, id, token, false))
	}
	return d.finishExternal(finishCtx, id, token, true)
}

func (d *Deduplicator) claimExternal(
	ctx context.Context, eventID string, payload []byte, token string, ttl time.Duration,
) (string, error) {
	var id string
	err := d.pool.QueryRow(ctx, `
		INSERT INTO inbox_events (id, consumer, event_id, payload, claim_token, claimed_until)
		VALUES ($1, $2, $3, $4::jsonb, $1,
		        clock_timestamp() + $5::bigint * interval '1 microsecond')
		ON CONFLICT (consumer, event_id) DO UPDATE
		SET claim_token = EXCLUDED.claim_token, claimed_until = EXCLUDED.claimed_until
		WHERE inbox_events.processed_at IS NULL
		  AND (inbox_events.claim_token IS NULL OR inbox_events.claimed_until <= clock_timestamp())
		RETURNING id`,
		token, d.consumer, eventID, payload, ttl.Microseconds(),
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("claim inbox event %q: %w", eventID, err)
	}
	var processed bool
	if err := d.pool.QueryRow(ctx, `
		SELECT processed_at IS NOT NULL FROM inbox_events
		WHERE consumer = $1 AND event_id = $2`, d.consumer, eventID).Scan(&processed); err != nil {
		return "", fmt.Errorf("check inbox event %q: %w", eventID, err)
	}
	if processed {
		return "", nil
	}
	return "", fmt.Errorf("inbox event %q: %w", eventID, ErrClaimInProgress)
}

func (d *Deduplicator) renewExternal(
	ctx context.Context, stop <-chan struct{}, id, token string, ttl time.Duration,
) error {
	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		renewCtx, cancel := context.WithTimeout(ctx, ttl/3)
		tag, err := d.pool.Exec(renewCtx, `
			UPDATE inbox_events
			SET claimed_until = clock_timestamp() + $3::bigint * interval '1 microsecond'
			WHERE id = $1 AND claim_token = $2 AND processed_at IS NULL
			  AND claimed_until > clock_timestamp()`, id, token, ttl.Microseconds())
		cancel()
		// Normal handler completion can interrupt an in-flight renewal.
		select {
		case <-stop:
			return nil
		case <-ctx.Done():
			return nil
		default:
		}
		if err != nil {
			return fmt.Errorf("renew inbox claim: %w", errors.Join(ErrClaimLost, err))
		}
		if tag.RowsAffected() == 0 {
			return ErrClaimLost
		}
	}
}

func (d *Deduplicator) finishExternal(ctx context.Context, id, token string, processed bool) error {
	tag, err := d.pool.Exec(ctx, `
		UPDATE inbox_events
		SET processed_at = CASE WHEN $3 THEN clock_timestamp() ELSE NULL END,
		    claim_token = NULL, claimed_until = NULL
		WHERE id = $1 AND claim_token = $2 AND processed_at IS NULL
		  AND claimed_until > clock_timestamp()`, id, token, processed)
	if err != nil {
		return fmt.Errorf("finalize inbox claim: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrClaimLost
	}
	return nil
}
