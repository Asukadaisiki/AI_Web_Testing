package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type ServiceHealth struct {
	Role          string     `json:"role"`
	InstanceID    string     `json:"instance_id"`
	Scope         string     `json:"scope"`
	Healthy       bool       `json:"healthy"`
	Alive         bool       `json:"alive"`
	ObservedAt    time.Time  `json:"observed_at"`
	LastHealthyAt *time.Time `json:"last_healthy_at"`
}

// RecordServiceHeartbeat is trusted process telemetry, not a public API.
// Empty project means deployment scope. Freshness uses database time, 15s TTL.
func (s *Store) RecordServiceHeartbeat(ctx context.Context, role, instance, project string, healthy bool) error {
	if s == nil || s.pool == nil {
		return errors.New("service telemetry store is unavailable")
	}
	if (role != "api" && role != "outbox_relay" && role != "exporter") || strings.TrimSpace(instance) == "" {
		return errors.New("service telemetry identity is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO service_heartbeats(role,instance_id,project_id,healthy,last_healthy_at)
		VALUES($1,$2,$3,$4,CASE WHEN $4 THEN clock_timestamp() END)
		ON CONFLICT(role,instance_id,project_id) DO UPDATE
		SET healthy=EXCLUDED.healthy,observed_at=clock_timestamp(),
		    last_healthy_at=CASE WHEN EXCLUDED.healthy THEN clock_timestamp() ELSE service_heartbeats.last_healthy_at END`,
		role, instance, project, healthy)
	return err
}

// StartServiceHeartbeat observes process liveness every 5s. Stop and wait before
// closing the pool. This does not assert successful ingestion or dependency health.
func (s *Store) StartServiceHeartbeat(ctx context.Context, role, instance, project string) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			_ = s.RecordServiceHeartbeat(ctx, role, instance, project, true)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

// NewServiceObserver records completed iterations at most every 5s, but reports
// health transitions immediately. No successful iteration means no heartbeat.
func (s *Store) NewServiceObserver(role, instance, project string) func(context.Context, error) {
	var mu sync.Mutex
	var last time.Time
	var lastHealthy bool
	return func(ctx context.Context, outcome error) {
		healthy := outcome == nil
		mu.Lock()
		defer mu.Unlock()
		if !last.IsZero() && time.Since(last) < 5*time.Second && healthy == lastHealthy {
			return
		}
		if err := s.RecordServiceHeartbeat(ctx, role, instance, project, healthy); err == nil {
			last, lastHealthy = time.Now(), healthy
		}
	}
}

// ObserveLeaseResult must be deferred before transaction rollback is deferred,
// so telemetry runs after locks are released. It never changes the lease result.
// Resolve ownership from the environment, not a caller-supplied project ID.
func (s *Store) ObserveLeaseResult(ctx context.Context, environmentID string, result error) {
	if s == nil || s.pool == nil || environmentID == "" {
		return
	}
	var name string
	switch {
	case errors.Is(result, ErrCapacityBusy):
		name = "lease_contention"
	case errors.Is(result, ErrStaleFence):
		name = "stale_fencing_rejections"
	default:
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond)
	defer cancel()
	_, _ = s.pool.Exec(ctx, `INSERT INTO service_counters(project_id,name,value)
		SELECT project_id,$2,1 FROM environments WHERE id=$1
		ON CONFLICT(project_id,name) DO UPDATE SET value=service_counters.value+1,last_observed_at=clock_timestamp()`,
		environmentID, name)
}

func (s *Store) readServicesTelemetry(ctx context.Context, project string, view *WorkerServices) error {
	var available bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('service_heartbeats') IS NOT NULL
		AND to_regclass('service_counters') IS NOT NULL`).Scan(&available); err != nil {
		return err
	}
	if !available {
		return nil
	}
	view.ServicesTelemetryAvailable = true
	rows, err := s.pool.Query(ctx, `SELECT role,instance_id,CASE WHEN project_id='' THEN 'deployment_process' ELSE 'project_process' END,
		healthy,observed_at>clock_timestamp()-interval '15 seconds',observed_at,last_healthy_at
		FROM service_heartbeats WHERE project_id='' OR project_id=$1
		ORDER BY observed_at DESC,role,instance_id LIMIT 100`, project)
	if err != nil {
		return err
	}
	for rows.Next() {
		var health ServiceHealth
		if err := rows.Scan(&health.Role, &health.InstanceID, &health.Scope, &health.Healthy, &health.Alive, &health.ObservedAt, &health.LastHealthyAt); err != nil {
			rows.Close()
			return err
		}
		view.ServiceHealth = append(view.ServiceHealth, health)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return s.pool.QueryRow(ctx, `SELECT
		(SELECT value FROM service_counters WHERE project_id=$1 AND name='lease_contention'),
		(SELECT value FROM service_counters WHERE project_id=$1 AND name='stale_fencing_rejections')`, project).
		Scan(&view.LeaseContention, &view.StaleFencingRejections)
}
