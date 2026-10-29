package v3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/serviceprobe"
)

type workerAPIRepository struct {
	Repository
	project string
	drained bool
	view    postgres.WorkerServices
}

func (r *workerAPIRepository) GetWorkerServices(_ context.Context, project string) (postgres.WorkerServices, error) {
	r.project = project
	if r.view.PendingOutbox != 0 {
		return r.view, nil
	}
	return postgres.WorkerServices{Workers: []postgres.WorkerNode{{ID: "browser", Capacity: 2, InUse: 1}}}, nil
}
func (r *workerAPIRepository) SetWorkerDraining(_ context.Context, project, id, actor string, drain bool) (postgres.WorkerNode, error) {
	r.project, r.drained = project, drain
	return postgres.WorkerNode{ID: id, ProjectID: project, Draining: drain}, nil
}

func TestWorkerServicesRequiresScopedAdminAndBooleanDrain(t *testing.T) {
	for _, tc := range []struct {
		method, path, role, body string
		status                   int
	}{
		{"GET", "/api/v1/projects/p/services", "member", "", 403},
		{"GET", "/api/v1/projects/p/services", "admin", "", 200},
		{"PATCH", "/api/v1/projects/p/workers/b", "admin", `{}`, 400},
		{"PATCH", "/api/v1/projects/p/workers/b", "admin", `{"draining":true}`, 200},
	} {
		t.Run(tc.method+tc.role+tc.body, func(t *testing.T) {
			repo := &workerAPIRepository{}
			s := &Server{repository: repo}
			mux := http.NewServeMux()
			s.registerWorkerRoutes(mux)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), bridgeIdentityKey{}, BridgeIdentity{ProjectID: "p", ActorID: "actor", Role: tc.role}))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Code == 200 && repo.project != "p" {
				t.Fatalf("project scope lost: %s", repo.project)
			}
			if tc.method == "GET" && w.Code == 200 {
				var view map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view["stream"] == nil || view["dependencies"] == nil {
					t.Fatalf("missing measured service metrics: %s %v", w.Body.String(), err)
				}
			}
		})
	}
}

func TestWorkerServicesReportsUnknownMetricsWithoutFalseZero(t *testing.T) {
	age := 95.0
	repo := &workerAPIRepository{view: postgres.WorkerServices{PendingOutbox: 2, OldestOutboxSeconds: &age}}
	s := &Server{repository: repo}
	mux := http.NewServeMux()
	s.registerWorkerRoutes(mux)
	req := httptest.NewRequest("GET", "/api/v1/projects/p/services?topic=untrusted&group=untrusted", nil)
	req = req.WithContext(context.WithValue(req.Context(), bridgeIdentityKey{}, BridgeIdentity{ProjectID: "p", ActorID: "actor", Role: "admin"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var view struct {
		Observability map[string]struct {
			Status string   `json:"status"`
			Scope  string   `json:"scope"`
			Reason string   `json:"reason"`
			Value  *float64 `json:"value"`
		} `json:"observability"`
		Dependencies map[string]healthDependency `json:"dependencies"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kafka_consumer_lag", "exporter_health", "lease_contention", "stale_fencing_rejections"} {
		metric, ok := view.Observability[name]
		if !ok || metric.Status != "unmeasured" || metric.Value != nil || metric.Reason == "" || metric.Scope == "" {
			t.Fatalf("%s must be explicitly unmeasured, got %+v; body=%s", name, metric, w.Body.String())
		}
	}
	if view.Dependencies["api"].Status != "ok" || view.Dependencies["outbox_relay"].Status != "degraded" {
		t.Fatalf("missing API/queue age health: %s", w.Body.String())
	}
	if metric := view.Observability["outbox_relay_health"]; metric.Scope != "project_queue" || metric.Reason == "" {
		t.Fatalf("relay health must distinguish queue evidence from process liveness: %+v", metric)
	}
}

func TestWorkerServicesOptionalTrustedLagProbe(t *testing.T) {
	value := 8.0
	s := &Server{repository: &workerAPIRepository{}, health: HealthDependencies{
		KafkaLag: func(context.Context) serviceprobe.Measurement {
			return serviceprobe.Measurement{Status: "measured", Scope: "deployment_consumer_group",
				ConsumerGroup: "trusted-group", Topic: "trusted-topic", Value: &value}
		},
	}}
	mux := http.NewServeMux()
	s.registerWorkerRoutes(mux)
	req := httptest.NewRequest("GET", "/api/v1/projects/p/services?topic=attacker&group=attacker", nil)
	req = req.WithContext(context.WithValue(req.Context(), bridgeIdentityKey{}, BridgeIdentity{ProjectID: "p", ActorID: "actor", Role: "admin"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var result struct {
		Observability map[string]serviceprobe.Measurement `json:"observability"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	got := result.Observability["kafka_consumer_lag"]
	if got.Value == nil || *got.Value != 8 || got.ConsumerGroup != "trusted-group" || got.Topic != "trusted-topic" {
		t.Fatalf("trusted probe not forwarded: %s", w.Body.String())
	}
}

func TestWorkerServicesMeasuredProcessHealthKeepsQueueHealth(t *testing.T) {
	now, age, counter := time.Now().UTC(), 45.0, int64(2)
	view := postgres.WorkerServices{
		PendingOutbox: 1, OldestOutboxSeconds: &age, LeaseContention: &counter,
		ServiceHealth: []postgres.ServiceHealth{
			{Role: "exporter", Scope: "project_process", Healthy: true, Alive: false, ObservedAt: now},
			{Role: "outbox_relay", Scope: "deployment_process", Healthy: true, Alive: true, ObservedAt: now},
		},
	}
	metrics := serviceObservations(view)
	if metric := metrics["exporter_health"]; metric.Status != "down" || metric.ObservedAt == nil {
		t.Fatalf("expired exporter reported healthy: %+v", metric)
	}
	if metric := metrics["outbox_relay_health"]; metric.Status != "ok" || metric.Scope != "deployment_process" {
		t.Fatalf("live relay not measured: %+v", metric)
	}
	if metric := metrics["outbox_queue_health"]; metric.Status != "degraded" || metric.Value == nil || *metric.Value != age {
		t.Fatalf("process liveness hid old queue: %+v", metric)
	}
	if metric := metrics["lease_contention"]; metric.Status != "measured" || metric.Value == nil || *metric.Value != 2 {
		t.Fatalf("durable counter not projected: %+v", metric)
	}
}
