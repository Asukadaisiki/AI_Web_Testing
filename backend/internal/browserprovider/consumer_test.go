package browserprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type interruptedLifecycle struct {
	command   postgres.EnvironmentCommand
	completed bool
}

func (s *interruptedLifecycle) ProcessEnvironmentCommand(context.Context, event.Envelope[postgres.EnvironmentCommand], postgres.EnvironmentProvider) error {
	return postgres.ErrConflict
}
func (s *interruptedLifecycle) GetEnvironmentCommandClaim(context.Context, string) (postgres.EnvironmentCommandClaim, error) {
	return postgres.EnvironmentCommandClaim{Token: "same", Command: s.command}, nil
}
func (s *interruptedLifecycle) CompleteEnvironmentCommand(_ context.Context, c postgres.EnvironmentCommandClaim, _ postgres.EnvironmentProviderResult) error {
	if c.Token != "same" {
		return postgres.ErrConflict
	}
	s.completed = true
	return nil
}

type countingProvider struct{ calls int }

func (p *countingProvider) ApplyEnvironment(context.Context, postgres.EnvironmentCommand) (postgres.EnvironmentProviderResult, error) {
	p.calls++
	return postgres.EnvironmentProviderResult{}, nil
}
func TestLifecycleRecoveryRetainsCanonicalClaimAndRejectsAlteredDelivery(t *testing.T) {
	store := &interruptedLifecycle{command: postgres.EnvironmentCommand{EnvironmentID: "env", Kind: "reset", ProfileGeneration: 2, FencingToken: 3}}
	provider := &countingProvider{}
	envelope := event.Envelope[postgres.EnvironmentCommand]{EventID: "evt", Payload: store.command}
	if err := processLifecycle(context.Background(), store, envelope, provider); err != nil || !store.completed || provider.calls != 1 {
		t.Fatalf("recovery %v", err)
	}
	envelope.Payload.ProfileGeneration = 1
	if err := processLifecycle(context.Background(), store, envelope, provider); !errors.Is(err, postgres.ErrConflict) || provider.calls != 1 {
		t.Fatalf("altered delivery %v calls%d", err, provider.calls)
	}
}

type fakeEnvironmentBinder struct {
	err                 error
	environment, worker string
}

func (b *fakeEnvironmentBinder) BindEnvironmentWorker(_ context.Context, environment, worker string) error {
	b.environment, b.worker = environment, worker
	return b.err
}

func (b *fakeEnvironmentBinder) EnvironmentWorkerID(context.Context, string) (string, error) {
	return b.worker, b.err
}

func TestProvisionRequiresDurableProviderAffinityBeforeExternalWork(t *testing.T) {
	binder := &fakeEnvironmentBinder{err: postgres.ErrConflict}
	provider := &countingProvider{}
	bound := affineProvider{binder: binder, provider: provider, workerID: "browser_a"}
	command := postgres.EnvironmentCommand{EnvironmentID: "env", Kind: "provision"}
	if _, err := bound.ApplyEnvironment(context.Background(), command); !errors.Is(err, postgres.ErrConflict) || provider.calls != 0 {
		t.Fatalf("unbound provisioning: err=%v calls=%d", err, provider.calls)
	}
	binder.err = nil
	if _, err := bound.ApplyEnvironment(context.Background(), command); err != nil || provider.calls != 1 ||
		binder.environment != "env" || binder.worker != "browser_a" {
		t.Fatalf("bound provisioning: err=%v calls=%d binding=%+v", err, provider.calls, binder)
	}
}
