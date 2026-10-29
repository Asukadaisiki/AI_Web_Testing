package browserprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

// ConsumeLifecycle retries delivery without acknowledging provider failures.
// PostgreSQL keeps both claims and lifecycle facts authoritative.
func ConsumeLifecycle(ctx context.Context, bus eventbus.EventBus, store *postgres.Store, provider *Client, onError func(error)) error {
	boundProvider := affineProvider{binder: store, provider: provider, workerID: provider.WorkerID()}
	for ctx.Err() == nil {
		err := bus.Consume(ctx, postgres.EnvironmentCommandsTopic, postgres.EnvironmentCommandConsumer, func(ctx context.Context, message eventbus.Message) error {
			var envelope event.Envelope[postgres.EnvironmentCommand]
			if json.Unmarshal(message.Value, &envelope) != nil {
				return errors.New("invalid environment command")
			}
			return processLifecycle(ctx, store, envelope, boundProvider)
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && onError != nil {
			onError(err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

type environmentBinder interface {
	BindEnvironmentWorker(context.Context, string, string) error
	EnvironmentWorkerID(context.Context, string) (string, error)
}

// The provider is called only after the canonical lifecycle claim, and local
// browser identity is bound before the first profile is provisioned.
type affineProvider struct {
	binder   environmentBinder
	provider postgres.EnvironmentProvider
	workerID string
}

func (p affineProvider) ApplyEnvironment(ctx context.Context, command postgres.EnvironmentCommand) (postgres.EnvironmentProviderResult, error) {
	if command.Kind == "provision" {
		if err := p.binder.BindEnvironmentWorker(ctx, command.EnvironmentID, p.workerID); err != nil {
			return postgres.EnvironmentProviderResult{}, err
		}
	} else {
		id, err := p.binder.EnvironmentWorkerID(ctx, command.EnvironmentID)
		if err != nil {
			return postgres.EnvironmentProviderResult{}, err
		}
		if id != p.workerID {
			return postgres.EnvironmentProviderResult{}, postgres.ErrConflict
		}
	}
	return p.provider.ApplyEnvironment(ctx, command)
}

type lifecycleStore interface {
	ProcessEnvironmentCommand(context.Context, event.Envelope[postgres.EnvironmentCommand], postgres.EnvironmentProvider) error
	GetEnvironmentCommandClaim(context.Context, string) (postgres.EnvironmentCommandClaim, error)
	CompleteEnvironmentCommand(context.Context, postgres.EnvironmentCommandClaim, postgres.EnvironmentProviderResult) error
}

// An interrupted provider lifecycle call is reconciled using its existing
// canonical identity. Provision/reset/delete are idempotent at the provider;
// Run execution never uses this recovery path.
func processLifecycle(ctx context.Context, store lifecycleStore, envelope event.Envelope[postgres.EnvironmentCommand], provider postgres.EnvironmentProvider) error {
	err := store.ProcessEnvironmentCommand(ctx, envelope, provider)
	if !errors.Is(err, postgres.ErrConflict) {
		return err
	}
	claim, claimErr := store.GetEnvironmentCommandClaim(ctx, envelope.EventID)
	if claimErr != nil {
		return err
	}
	canonical, _ := json.Marshal(claim.Command)
	delivered, _ := json.Marshal(envelope.Payload)
	if !bytes.Equal(canonical, delivered) {
		return postgres.ErrConflict
	}
	result, err := provider.ApplyEnvironment(ctx, claim.Command)
	if err != nil {
		return err
	}
	return store.CompleteEnvironmentCommand(ctx, claim, result)
}
