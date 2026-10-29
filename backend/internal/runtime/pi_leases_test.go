package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/consumer"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestPiRunnerCapacityBusyIsDeferred(t *testing.T) {
	s := &fakePiStore{startErr: postgres.ErrCapacityBusy}
	p := &fakeHarnessProcess{}
	err := (PiRunner{Store: s, Process: p}).Run(context.Background(), commandFor("busy"))
	if !consumer.IsDeliveryDeferred(err) || !errors.Is(err, postgres.ErrCapacityBusy) || p.calls != 0 {
		t.Fatalf("busy delivery started or consumed failure budget: %v %d", err, p.calls)
	}
}

type leasePiStore struct {
	fakePiStore
	marked    bool
	released  bool
	abandoned bool
	renewErr  error
}

func (s *leasePiStore) MarkRunStarted(context.Context, HarnessAttemptRef) error {
	s.marked = true
	return nil
}
func (s *leasePiStore) RenewRunLease(context.Context, HarnessAttemptRef, time.Duration) error {
	return s.renewErr
}
func (s *leasePiStore) ReleaseRunLease(_ context.Context, _ HarnessAttemptRef, ack bool) error {
	s.released = ack
	return nil
}
func (s *leasePiStore) AbandonUnstartedRun(context.Context, HarnessAttemptRef) error {
	s.abandoned = true
	return nil
}

type releaseAwareProcess struct {
	fakeHarnessProcess
	ack   bool
	block bool
}

func (p *releaseAwareProcess) RunWithRelease(ctx context.Context, request piharness.Request, onEvent func(piharness.Event)) (bool, error) {
	if p.block {
		p.request = request
		<-ctx.Done()
		return p.ack, ctx.Err()
	}
	return p.ack, p.Run(ctx, request, onEvent)
}

func managedPiFixture() *leasePiStore {
	return &leasePiStore{fakePiStore: fakePiStore{ref: HarnessAttemptRef{
		ID: "hat", RunID: "managed", ProviderAttemptID: "provider", WorkerURL: "http://assigned:8100",
		Fence: postgres.HarnessFence{ProjectID: "p", EnvironmentID: "env", RunID: "managed", AttemptID: "hat",
			RuntimeWorkerID: "r", BrowserWorkerID: "b", ProfileGeneration: 3, FencingToken: 8},
	}}}
}

func TestPiRunnerUsesAssignmentMetadataAndExplicitReleaseAck(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(map[bool]string{false: "unacknowledged", true: "acknowledged"}[ack], func(t *testing.T) {
			s := managedPiFixture()
			p := &releaseAwareProcess{ack: ack, fakeHarnessProcess: fakeHarnessProcess{events: passingEventStream("managed", "provider")}}
			command := commandFor("managed")
			command.WorkerURL = "http://untrusted-snapshot:8100"
			err := (PiRunner{Store: s, Process: p, WorkerURL: "http://static:8100"}).Run(context.Background(), command)
			if p.request.WorkerURL != s.ref.WorkerURL || p.request.EnvironmentID != "env" || p.request.ProjectID != "p" ||
				p.request.ProfileGeneration != 3 || p.request.FencingToken != 8 || !s.marked {
				t.Fatalf("assignment not honored: %#v marked=%v err=%v", p.request, s.marked, err)
			}
			if s.released != ack {
				t.Fatalf("release acknowledgement not honored: released=%v ack=%v", s.released, ack)
			}
			if ack && err != nil {
				t.Fatalf("acknowledged execution failed: %v", err)
			}
			if !ack && err == nil {
				t.Fatal("unacknowledged execution reported success")
			}
		})
	}
}

func TestPiRunnerLeaseLossCancelsSDKAndDoesNotFinalizeStaleOutcome(t *testing.T) {
	s := managedPiFixture()
	s.renewErr = postgres.ErrStaleFence
	p := &releaseAwareProcess{ack: true, block: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (PiRunner{Store: s, Process: p, LeaseTTL: 15 * time.Millisecond}).Run(ctx, commandFor("managed"))
	if !errors.Is(err, postgres.ErrStaleFence) || s.completed || s.released {
		t.Fatalf("stale run finalized: err=%v complete=%v release=%v", err, s.completed, s.released)
	}
}

func TestPiRunnerPreCancelledClaimNeverInventsHostReleaseAck(t *testing.T) {
	s := managedPiFixture()
	s.ref.CancelRequested = true
	p := &releaseAwareProcess{}
	err := (PiRunner{Store: s, Process: p}).Run(context.Background(), commandFor("managed"))
	if !isTerminalRunError(err) || p.calls != 0 || s.marked || s.released || !s.abandoned {
		t.Fatalf("pre-cancelled claim fabricated host lifecycle: err=%v calls=%d marked=%v released=%v abandoned=%v", err, p.calls, s.marked, s.released, s.abandoned)
	}
}
