package v3

import (
	"context"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/serviceprobe"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
)

type WorkerRepository interface {
	GetWorkerServices(context.Context, string) (postgres.WorkerServices, error)
	SetWorkerDraining(context.Context, string, string, string, bool) (postgres.WorkerNode, error)
}

func (s *Server) registerWorkerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{id}/services", s.handleWorkerServices)
	mux.HandleFunc("PATCH /api/v1/projects/{id}/workers/{workerId}", s.handleWorkerDrain)
}

func (s *Server) workerScope(w http.ResponseWriter, r *http.Request) (WorkerRepository, BridgeIdentity, bool) {
	identity, ok := BridgeIdentityFromContext(r.Context())
	if !ok || identity.ProjectID != r.PathValue("id") || identity.ActorID == "" ||
		(identity.Role != "admin" && identity.Role != "project_admin" && identity.Role != "system_admin") {
		writeBridgeForbidden(w)
		return nil, identity, false
	}
	repo, ok := s.repository.(WorkerRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "workers_unavailable", "Worker management is unavailable")
		return nil, identity, false
	}
	return repo, identity, true
}

func (s *Server) handleWorkerServices(w http.ResponseWriter, r *http.Request) {
	repo, identity, ok := s.workerScope(w, r)
	if !ok {
		return
	}
	view, err := repo.GetWorkerServices(r.Context(), identity.ProjectID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	checks := []struct {
		name     string
		probe    func(context.Context) error
		required bool
	}{
		{"postgres", s.health.Postgres, true}, {"kafka", s.health.Kafka, false},
		{"redis", s.health.Redis, false}, {"browser_worker", s.health.Worker, false},
		{"langfuse", s.health.Langfuse, false},
	}
	type result struct {
		name       string
		dependency healthDependency
	}
	results := make(chan result, len(checks))
	for _, check := range checks {
		go func() {
			results <- result{check.name, checkDependency(r.Context(), check.probe, check.required)}
		}()
	}
	dependencies := make(map[string]healthDependency, len(checks))
	for range checks {
		check := <-results
		dependencies[check.name] = check.dependency
	}
	kafka := dependencies["kafka"]
	kafka.PendingOutbox = view.PendingOutbox
	dependencies["kafka"] = kafka
	if dependencies["redis"].Status != "ok" {
		redis := dependencies["redis"]
		redis.Mode = "postgres_poll"
		dependencies["redis"] = redis
	}
	observations := serviceObservations(view)
	if s.health.KafkaLag != nil {
		observations["kafka_consumer_lag"] = s.health.KafkaLag(r.Context())
	}
	dependencies["api"] = healthDependency{Status: "ok", Mode: "api_instance"}
	dependencies["outbox_relay"] = healthDependency{
		Status: observations["outbox_relay_health"].Status,
		Mode:   "project_queue_age_only", PendingOutbox: view.PendingOutbox,
	}
	dependencies["exporter"] = healthDependency{Status: "unmeasured", Mode: "process_liveness_not_instrumented"}
	dependencies["outbox_queue"] = healthDependency{Status: observations["outbox_queue_health"].Status,
		Mode: "project_queue_age", PendingOutbox: view.PendingOutbox}
	for _, role := range []string{"exporter", "outbox_relay"} {
		if metric := observations[role+"_health"]; metric.Scope == "deployment_process" || metric.Scope == "project_process" {
			dependencies[role] = healthDependency{Status: metric.Status, Mode: metric.Reason}
		}
	}
	writeJSON(w, http.StatusOK, struct {
		postgres.WorkerServices
		Stream        stream.Stats                        `json:"stream"`
		StreamScope   string                              `json:"stream_scope"`
		Dependencies  map[string]healthDependency         `json:"dependencies"`
		Observability map[string]serviceprobe.Measurement `json:"observability"`
	}{view, s.stream.Stats(), "api_instance", dependencies, observations})
}

func serviceObservations(view postgres.WorkerServices) map[string]serviceprobe.Measurement {
	unknown := func(scope, reason string) serviceprobe.Measurement {
		return serviceprobe.Measurement{Status: "unmeasured", Scope: scope, Reason: reason}
	}
	metrics := map[string]serviceprobe.Measurement{
		"kafka_consumer_lag":       unknown("deployment_consumer_group", "probe_not_configured"),
		"exporter_health":          unknown("deployment_process", "exporter_heartbeat_not_instrumented"),
		"lease_contention":         unknown("project", "lease_conflict_counter_not_instrumented"),
		"stale_fencing_rejections": unknown("project", "fencing_rejection_counter_not_instrumented"),
		"outbox_relay_health":      unknown("project_queue", "queue_age_unavailable; relay_process_liveness_unmeasured"),
		"dlq_count":                unknown("project_retained_outbox_handoffs", "durable_handoff_count_unavailable"),
	}
	if view.ServicesTelemetryAvailable {
		metrics["exporter_health"] = unknown("deployment_process", "no_exporter_heartbeat_observed")
		metrics["lease_contention"] = unknown("project", "no_durable_counter_observations")
		metrics["stale_fencing_rejections"] = unknown("project", "no_durable_counter_observations")
	}
	if view.DLQCount != nil {
		count := float64(*view.DLQCount)
		metrics["dlq_count"] = serviceprobe.Measurement{Status: "measured", Scope: view.DLQScope, Value: &count,
			Reason: "retained_handoffs_for_current_project_aggregates; excludes_kafka_only_and_deleted_aggregates"}
	}
	relay := metrics["outbox_relay_health"]
	switch {
	case view.PendingOutbox == 0:
		relay.Status, relay.Reason = "no_backlog", "no_pending_project_rows; relay_process_liveness_unmeasured"
	case view.OldestOutboxSeconds != nil:
		relay.Value = view.OldestOutboxSeconds
		relay.Status = "backlog"
		if *view.OldestOutboxSeconds >= 30 {
			relay.Status = "degraded"
		}
		relay.Reason = "oldest_pending_age_seconds; degraded_at_30_seconds; relay_process_liveness_unmeasured"
	}
	metrics["outbox_relay_health"] = relay
	metrics["outbox_queue_health"] = relay
	for _, counter := range []struct {
		name  string
		value *int64
	}{
		{"lease_contention", view.LeaseContention},
		{"stale_fencing_rejections", view.StaleFencingRejections},
	} {
		if counter.value != nil {
			value := float64(*counter.value)
			metrics[counter.name] = serviceprobe.Measurement{Status: "measured", Scope: "project",
				Value: &value, Reason: "durable_best_effort_lease_api_rejections_since_instrumentation"}
		}
	}
	for _, role := range []string{"exporter", "outbox_relay"} {
		var selected *postgres.ServiceHealth
		for i := range view.ServiceHealth {
			health := &view.ServiceHealth[i]
			if health.Role != role {
				continue
			}
			if selected == nil || (health.Alive && health.Healthy && !(selected.Alive && selected.Healthy)) {
				selected = health
			}
		}
		if selected != nil {
			metric := serviceprobe.Measurement{Status: "down", Scope: selected.Scope,
				ObservedAt: &selected.ObservedAt, Reason: "heartbeat_expired_15_seconds"}
			if selected.Alive {
				metric.Status, metric.Reason = "degraded", "last_completed_iteration_failed"
				if selected.Healthy {
					metric.Status, metric.Reason = "ok", "completed_iteration_heartbeat"
				}
			}
			if role == "exporter" {
				metric.Reason = "process_liveness_heartbeat; does_not_assert_export_delivery; " + metric.Reason
			}
			metrics[role+"_health"] = metric
		}
	}
	return metrics
}

func (s *Server) handleWorkerDrain(w http.ResponseWriter, r *http.Request) {
	repo, identity, ok := s.workerScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Draining *bool `json:"draining"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if input.Draining == nil {
		writeError(w, http.StatusBadRequest, "invalid_worker", "draining must be a boolean")
		return
	}
	project := identity.ProjectID
	if identity.Role == "system_admin" {
		// The authenticated system caller may drain shared deployment nodes.
		project = ""
	}
	node, err := repo.SetWorkerDraining(r.Context(), project, r.PathValue("workerId"), identity.ActorID, *input.Draining)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}
