package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/eventbus"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/outbox"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
	"github.com/jackc/pgx/v5/pgxpool"
)

func servicesTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store) {
	t.Helper()
	dsn := os.Getenv("AGENTTRACE_SERVICES_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTTRACE_SERVICES_TEST_POSTGRES_DSN is not configured")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid services test DSN")
	}
	name, err := repositoryTestDatabaseName(dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"AGENTTRACE_TEST_POSTGRES_DSN", "AGENTTRACE_LEASE_TEST_POSTGRES_DSN", "AGENTTRACE_MANAGEMENT_TEST_POSTGRES_DSN", "AGENTTRACE_CONTROL_TEST_POSTGRES_DSN", "AGENTTRACE_SEED_TEST_POSTGRES_DSN"} {
		if other := os.Getenv(key); other != "" {
			otherName, _ := repositoryTestDatabaseName(other)
			if name == otherName {
				t.Fatalf("services tests require a database independent of %s", key)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect services test database failed")
	}
	t.Cleanup(pool.Close)
	var actual string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != cfg.ConnConfig.Database {
		t.Fatal("services test database identity check failed")
	}
	resetDatabase(t, ctx, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)

	return ctx, pool, store
}

func TestServicesObservabilityDurableDLQAndAuditAreProjectScoped(t *testing.T) {
	ctx, pool, store := servicesTestStore(t)
	first := boundConversation(t, ctx, store)
	project, err := store.CreateProject(ctx, CreateProjectParams{Name: "Other", LangfuseProjectID: "lf_other"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateConversation(ctx, CreateConversationParams{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []Conversation{first, second} {
		runID := "run_" + c.ID
		if _, err := pool.Exec(ctx, `INSERT INTO runs(id,project_id,conversation_id,status,specification) VALUES($1,$2,$3,'failed','{}')`, runID, c.ProjectID, c.ID); err != nil {
			t.Fatal(err)
		}
		// Published and pending handoffs both remain durable DLQ facts. The
		// second topic also checks configured topic names, not a hardcoded default.
		for n, topic := range []string{"agenttrace.run.dlq.v1", "deployment.custom.dlq"} {
			payload := []byte(`{"original":{"event_id":"source"},"error":"credential=https://secret.invalid"}`)
			if _, err := store.IncrementConsumerRetry(ctx, "test", runID+topic, []byte(`{"event_id":"source"}`)); err != nil {
				t.Fatal(err)
			}
			if err := store.HandoffConsumerRetryToOutbox(ctx, "test", runID+topic, runID+topic, topic, runID, payload); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				if _, err := pool.Exec(ctx, `UPDATE outbox_events SET published_at=now() WHERE event_id=$1`, runID+topic); err != nil {
					t.Fatal(err)
				}
			}
		}
		skill, err := store.UploadSkill(ctx, c.ProjectID, "actor", "audit-skill", []registry.File{{Path: "SKILL.md", Content: "# Audit\nTest instructions"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishSkill(ctx, c.ProjectID, skill.ID, "publisher"); err != nil {
			t.Fatal(err)
		}
		if err := store.RevokeSkill(ctx, c.ProjectID, skill.ID, "revoker"); err != nil {
			t.Fatal(err)
		}
	}
	view, err := store.GetWorkerServices(ctx, first.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	var measured struct {
		DLQCount *int64 `json:"dlq_count"`
		DLQScope string `json:"dlq_scope"`
		Audit    []struct {
			ProjectID string `json:"project_id"`
			Action    string `json:"action"`
		} `json:"recent_registry_audit"`
	}
	if err := json.Unmarshal(raw, &measured); err != nil {
		t.Fatal(err)
	}
	if measured.DLQCount == nil || *measured.DLQCount != 2 || measured.DLQScope == "" {
		t.Fatalf("missing real retained DLQ measurement: %s", raw)
	}
	actions := map[string]bool{}
	for _, audit := range measured.Audit {
		if audit.ProjectID != first.ProjectID {
			t.Fatalf("foreign audit exposed: %s", raw)
		}
		actions[audit.Action] = true
	}
	if !actions["skill.publish"] || !actions["skill.revoke"] {
		t.Fatalf("publish/revoke audit missing: %s", raw)
	}
	if strings.Contains(string(raw), "secret.invalid") {
		t.Fatal("DLQ error payload leaked")
	}
}

func TestServicesObservabilityAuditIsBoundedAndZeroDLQIsMeasured(t *testing.T) {
	ctx, pool, store := servicesTestStore(t)
	c := boundConversation(t, ctx, store)
	for n := 0; n < 25; n++ {
		if _, err := pool.Exec(ctx, `INSERT INTO registry_audit(project_id,actor_id,action,resource_id,occurred_at)
			VALUES($1,'actor','skill.publish','resource',now()+$2*interval '1 second')`, c.ProjectID, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO registry_audit(project_id,actor_id,action,resource_id)
		VALUES(NULL,'system','mcp.publish','global'),($1,'actor','https://unsafe.invalid','unsafe')`, c.ProjectID); err != nil {
		t.Fatal(err)
	}
	view, err := store.GetWorkerServices(ctx, c.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if view.DLQCount == nil || *view.DLQCount != 0 || len(view.RecentRegistryAudit) != 20 {
		t.Fatalf("zero/bounded measurement=%+v", view)
	}
	for i, audit := range view.RecentRegistryAudit {
		if audit.ProjectID != c.ProjectID || audit.Action != "skill.publish" ||
			(i > 0 && !view.RecentRegistryAudit[i-1].OccurredAt.After(audit.OccurredAt)) {
			t.Fatalf("unscoped/unordered audit: %+v", view.RecentRegistryAudit)
		}
	}
}

func TestServicesObservabilityProcessHealthAndCountersHaveDurableEvidence(t *testing.T) {
	ctx, pool, store := servicesTestStore(t)
	c := boundConversation(t, ctx, store)
	env, err := store.GetConversationEnvironment(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "outbox_relay", "exporter"} {
		if err := store.RecordServiceHeartbeat(ctx, role, "owned-process", c.ProjectID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordServiceHeartbeat(ctx, "exporter", "foreign-process", "foreign-project", true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordServiceHeartbeat(ctx, "exporter", "owned-process", c.ProjectID, false); err != nil {
		t.Fatal(err)
	}
	store.ObserveLeaseResult(ctx, env.ID, ErrCapacityBusy)
	store.ObserveLeaseResult(ctx, env.ID, ErrStaleFence)
	store.ObserveLeaseResult(ctx, env.ID, ErrConflict)
	view, err := store.GetWorkerServices(ctx, c.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ServiceHealth) != 3 {
		t.Fatalf("service evidence missing/unscoped: %+v", view)
	}
	for _, health := range view.ServiceHealth {
		if health.InstanceID != "owned-process" || !health.Alive || health.LastHealthyAt == nil ||
			(health.Role == "exporter" && health.Healthy) {
			t.Fatalf("untruthful health: %+v", health)
		}
	}
	if view.LeaseContention == nil || *view.LeaseContention != 1 || view.StaleFencingRejections == nil || *view.StaleFencingRejections != 1 {
		t.Fatalf("missing rejection measurements: %+v", view)
	}
	if _, err := pool.Exec(ctx, `UPDATE service_heartbeats SET observed_at=clock_timestamp()-interval '30 seconds'
		WHERE instance_id='owned-process' AND project_id=$1`, c.ProjectID); err != nil {
		t.Fatal(err)
	}
	view, err = store.GetWorkerServices(ctx, c.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, health := range view.ServiceHealth {
		if health.Alive {
			t.Fatalf("expired heartbeat falsely healthy: %+v", health)
		}
	}
	// Optional telemetry failure must not change or block the original result.
	if _, err := pool.Exec(ctx, `DROP TABLE service_counters`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	store.ObserveLeaseResult(ctx, env.ID, ErrStaleFence)
	if time.Since(start) > time.Second {
		t.Fatal("failed optional telemetry blocked lease outcome")
	}
}

func TestServicesObservabilityRealLeaseRejectionsPreserveOwnership(t *testing.T) {
	ctx, pool, store := servicesTestStore(t)
	p := leaseFixture(t, ctx, pool, store, "services")
	registerLeaseWorkers(t, ctx, store, 2)
	claim, err := store.ClaimHarnessLease(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimHarnessLease(ctx, p); !errors.Is(err, ErrCapacityBusy) {
		t.Fatalf("busy result changed: %v", err)
	}
	stale := claim.Fence
	stale.FencingToken++
	if err := store.RenewHarnessLease(ctx, stale, time.Minute); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale result changed: %v", err)
	}
	view, err := store.GetWorkerServices(ctx, p.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if view.LeaseContention == nil || *view.LeaseContention != 1 || view.StaleFencingRejections == nil || *view.StaleFencingRejections != 1 {
		t.Fatalf("actual lease outcomes not instrumented: %+v", view)
	}
	if err := store.RenewHarnessLease(ctx, claim.Fence, time.Minute); err != nil {
		t.Fatalf("telemetry altered rightful lease: %v", err)
	}
}

type servicesBus struct {
	eventbus.EventBus
	err error
}

func (b servicesBus) Publish(context.Context, string, string, []byte) (eventbus.Metadata, error) {
	return eventbus.Metadata{}, b.err
}

func TestServicesObservabilityRelayRecordsCompletedIterationOnly(t *testing.T) {
	ctx, pool, store := servicesTestStore(t)
	c := boundConversation(t, ctx, store)
	cause := errors.New("safe test failure")
	relay := outbox.NewRelay(pool, servicesBus{err: cause}, outbox.Config{
		WorkerID: "observed", Observe: store.NewServiceObserver("outbox_relay", "observed", ""),
	})
	if _, err := relay.RelayOnce(ctx); err == nil {
		t.Fatal("telemetry swallowed relay failure")
	}
	view, err := store.GetWorkerServices(ctx, c.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.ServiceHealth) != 1 || view.ServiceHealth[0].Healthy || view.ServiceHealth[0].LastHealthyAt != nil {
		t.Fatalf("failed iteration falsely healthy: %+v", view.ServiceHealth)
	}
	relay = outbox.NewRelay(pool, servicesBus{}, outbox.Config{
		WorkerID: "observed", Observe: store.NewServiceObserver("outbox_relay", "observed", ""),
	})
	if _, err := relay.RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = store.GetWorkerServices(ctx, c.ProjectID)
	if err != nil || len(view.ServiceHealth) != 1 || !view.ServiceHealth[0].Healthy || view.ServiceHealth[0].LastHealthyAt == nil {
		t.Fatalf("successful iteration not measured: %+v %v", view.ServiceHealth, err)
	}
}
