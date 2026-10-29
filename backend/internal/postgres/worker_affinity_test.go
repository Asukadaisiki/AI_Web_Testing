package postgres

import (
	"errors"
	"testing"
)

func TestLeaseUnboundEnvironmentDoesNotAllocateArbitraryBrowser(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	request := leaseFixture(t, ctx, pool, s, "unbound")
	registerUnboundLeaseWorkers(t, ctx, s, 1)
	if _, err := s.EnvironmentWorkerID(ctx, request.EnvironmentID); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound provider lookup: %v", err)
	}
	if _, err := s.ClaimHarnessLease(ctx, request); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("unbound environment assigned a foreign profile provider: %v", err)
	}
	assertRowCount(t, ctx, pool, "harness_attempts", 0)
	var used int
	leaseTestScan(t, pool.QueryRow(ctx, `SELECT sum(in_use) FROM worker_nodes`), &used)
	if used != 0 {
		t.Fatalf("unbound environment consumed capacity: %d", used)
	}
}

func TestBrowserWorkerIdentityUsesCanonicalEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://Browser:80/", "http://browser", " http://browser/ "} {
		if got := BrowserWorkerID(endpoint); got == "" || got != BrowserWorkerID("http://browser") {
			t.Fatalf("equivalent endpoint %q identity = %q", endpoint, got)
		}
	}
	if BrowserWorkerID("http://browser/one") == BrowserWorkerID("http://browser/two") {
		t.Fatal("distinct provider routes have the same worker identity")
	}
	if BrowserWorkerID("http://user:secret@browser") != "" {
		t.Fatal("credential-bearing endpoint accepted")
	}
}

func TestEnvironmentProviderAffinityIsImmutableAndDoesNotFallback(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	request := leaseFixture(t, ctx, pool, s, "affinity")
	registerUnboundLeaseWorkers(t, ctx, s, 1)
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "other", Role: "browser", Endpoint: "http://other:8100", Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.BindEnvironmentWorker(ctx, request.EnvironmentID, "browser"); err != nil {
		t.Fatal(err)
	}
	if id, err := s.EnvironmentWorkerID(ctx, request.EnvironmentID); err != nil || id != "browser" {
		t.Fatalf("bound provider lookup: %q %v", id, err)
	}
	if err := s.BindEnvironmentWorker(ctx, request.EnvironmentID, "browser"); err != nil {
		t.Fatalf("binding replay: %v", err)
	}
	if err := s.BindEnvironmentWorker(ctx, request.EnvironmentID, "other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("local profile moved to another worker: %v", err)
	}
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "browser", Role: "browser", Endpoint: "http://replacement:8100", Capacity: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("bound provider endpoint replaced while idle: %v", err)
	}
	if _, err := s.SetWorkerDraining(ctx, "", "browser", "actor", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimHarnessLease(ctx, request); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("drained local profile provider fell back to another worker: %v", err)
	}
	assertRowCount(t, ctx, pool, "harness_attempts", 0)
	if _, err := s.SetWorkerDraining(ctx, "", "browser", "actor", false); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimHarnessLease(ctx, request)
	if err != nil || claim.Fence.BrowserWorkerID != "browser" || claim.WorkerURL != "http://trusted-browser:8100" {
		t.Fatalf("profile affinity claim: %#v %v", claim, err)
	}
}

func TestEnvironmentAffinityRejectsForeignAndRuntimeNodes(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	request := leaseFixture(t, ctx, pool, s, "scope")
	foreign := leaseFixture(t, ctx, pool, s, "foreign-scope")
	registerUnboundLeaseWorkers(t, ctx, s, 1)
	if _, err := s.RegisterWorker(ctx, WorkerRegistration{ID: "foreign", Role: "browser", ProjectID: foreign.ProjectID, Endpoint: "http://foreign:8100", Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"foreign", "runtime", "missing"} {
		if err := s.BindEnvironmentWorker(ctx, request.EnvironmentID, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalid binding %s: %v", id, err)
		}
	}
}

func TestWorkerReadinessTracksDurableDrainAndHeartbeatNotCapacity(t *testing.T) {
	ctx, pool, s := leaseTestStore(t)
	request := leaseFixture(t, ctx, pool, s, "readiness")
	registerLeaseWorkers(t, ctx, s, 1)
	if _, err := s.ClaimHarnessLease(ctx, request); err != nil {
		t.Fatal(err)
	}
	ready, err := s.WorkerReady(ctx, "runtime")
	if err != nil || !ready {
		t.Fatalf("full active worker must remain ready: %v %v", ready, err)
	}
	if _, err := s.SetWorkerDraining(ctx, "", "runtime", "actor", true); err != nil {
		t.Fatal(err)
	}
	ready, err = s.WorkerReady(ctx, "runtime")
	if err != nil || ready {
		t.Fatalf("drained worker ready: %v %v", ready, err)
	}
	if _, err := s.SetWorkerDraining(ctx, "", "runtime", "actor", false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE worker_nodes SET last_seen_at=clock_timestamp()-interval '2 minutes' WHERE id='runtime'`); err != nil {
		t.Fatal(err)
	}
	ready, err = s.WorkerReady(ctx, "runtime")
	if err != nil || ready {
		t.Fatalf("dead worker ready: %v %v", ready, err)
	}
}
