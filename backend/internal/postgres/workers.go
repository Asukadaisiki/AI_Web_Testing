package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type WorkerRegistration struct {
	ID           string
	Role         string
	ProjectID    string
	Endpoint     string
	Version      string
	RuntimeIDs   []string
	Capacity     int
	HeartbeatTTL time.Duration
}

// WorkerNode deliberately omits the internal endpoint from its product view.
type WorkerNode struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	ProjectID  string    `json:"project_id,omitempty"`
	Version    string    `json:"version"`
	RuntimeIDs []string  `json:"runtime_ids"`
	Capacity   int       `json:"capacity"`
	InUse      int       `json:"in_use"`
	Draining   bool      `json:"draining"`
	Alive      bool      `json:"alive"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

const workerViewColumns = `id, role, COALESCE(project_id,''), version, runtime_ids, capacity, in_use, draining,
  last_seen_at + heartbeat_ttl_ms * interval '1 millisecond' > clock_timestamp(), last_seen_at`

func scanWorker(row pgx.Row) (WorkerNode, error) {
	var w WorkerNode
	var ids []byte
	err := row.Scan(&w.ID, &w.Role, &w.ProjectID, &w.Version, &ids, &w.Capacity, &w.InUse, &w.Draining, &w.Alive, &w.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrNotFound
	}
	if err != nil {
		return w, err
	}
	return w, json.Unmarshal(ids, &w.RuntimeIDs)
}

// RegisterWorker is a trusted deployment operation, not a user endpoint.
// Re-registration preserves drain and occupied slots and cannot change identity
// or a browser endpoint. Changing a local profile provider needs a new node.
func (s *Store) RegisterWorker(ctx context.Context, p WorkerRegistration) (WorkerNode, error) {
	if err := s.requireManagement(); err != nil {
		return WorkerNode{}, err
	}
	if strings.TrimSpace(p.ID) == "" || p.Capacity < 1 || (p.Role != "runtime" && p.Role != "browser") {
		return WorkerNode{}, errors.New("worker id, role and positive capacity are required")
	}
	if p.Role == "browser" {
		var err error
		p.Endpoint, err = canonicalWorkerEndpoint(p.Endpoint)
		if err != nil {
			return WorkerNode{}, err
		}
	}
	if p.HeartbeatTTL == 0 {
		p.HeartbeatTTL = 30 * time.Second
	}
	if p.HeartbeatTTL < time.Millisecond {
		return WorkerNode{}, errors.New("worker heartbeat TTL must be at least one millisecond")
	}
	if p.RuntimeIDs == nil {
		p.RuntimeIDs = []string{}
	}
	ids, err := json.Marshal(p.RuntimeIDs)
	if err != nil {
		return WorkerNode{}, err
	}
	w, err := scanWorker(s.pool.QueryRow(ctx, `INSERT INTO worker_nodes(id,role,project_id,endpoint,version,runtime_ids,capacity,heartbeat_ttl_ms)
		VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8)
		ON CONFLICT(id) DO UPDATE SET endpoint=EXCLUDED.endpoint,version=EXCLUDED.version,
		  runtime_ids=EXCLUDED.runtime_ids,capacity=EXCLUDED.capacity,heartbeat_ttl_ms=EXCLUDED.heartbeat_ttl_ms,
		  last_seen_at=clock_timestamp(),updated_at=now()
		WHERE worker_nodes.role=EXCLUDED.role
		  AND worker_nodes.project_id IS NOT DISTINCT FROM EXCLUDED.project_id
		  AND worker_nodes.in_use <= EXCLUDED.capacity
		  AND (worker_nodes.in_use=0 OR worker_nodes.endpoint=EXCLUDED.endpoint)
		  AND (worker_nodes.role<>'browser' OR worker_nodes.endpoint=EXCLUDED.endpoint)
		RETURNING `+workerViewColumns, p.ID, p.Role, p.ProjectID, p.Endpoint, p.Version, ids, p.Capacity, p.HeartbeatTTL.Milliseconds()))
	if errors.Is(err, ErrNotFound) {
		return w, ErrConflict
	}
	return w, err
}

func canonicalWorkerEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("browser worker endpoint must be a trusted absolute HTTP URL")
	}
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "http" && u.Port() == "80" || u.Scheme == "https" && u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String(), nil
}

// BrowserWorkerID is shared by the API provider and runtime deployment.
// Empty means the endpoint is not valid for trusted worker registration.
func BrowserWorkerID(endpoint string) string {
	canonical, err := canonicalWorkerEndpoint(endpoint)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("browser-%x", digest[:12])
}

// BindEnvironmentWorker must precede provision/control. Profiles are local:
// once bound, even an idle environment cannot migrate to a different node.
func (s *Store) BindEnvironmentWorker(ctx context.Context, environmentID, workerID string) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	env, err := lockEnvironment(ctx, tx, environmentID)
	if err != nil {
		return err
	}
	var bound string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(browser_worker_id,'') FROM environments WHERE id=$1`, environmentID).Scan(&bound); err != nil {
		return err
	}
	if bound != "" && bound != workerID {
		return ErrConflict
	}
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM worker_nodes WHERE id=$1 AND role='browser'
		AND (project_id IS NULL OR project_id=$2) FOR UPDATE`, workerID, env.ProjectID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE environments SET browser_worker_id=$2,updated_at=now() WHERE id=$1`, environmentID, workerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// EnvironmentWorkerID is a trusted routing check for lifecycle/control callers,
// not a public endpoint or a way to move profile storage between providers.
func (s *Store) EnvironmentWorkerID(ctx context.Context, environmentID string) (string, error) {
	if err := s.requireManagement(); err != nil {
		return "", err
	}
	var id string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(browser_worker_id,'') FROM environments WHERE id=$1`, environmentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err == nil && id == "" {
		return "", ErrConflict
	}
	return id, err
}

func (s *Store) HeartbeatWorker(ctx context.Context, id string) error {
	if err := s.requireManagement(); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE worker_nodes SET last_seen_at=clock_timestamp(),updated_at=now() WHERE id=$1`, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

// WorkerReady deliberately ignores occupied slots. Busy delivery retries keep
// their owner; durable drain or lost heartbeat relinquishes Kafka ownership.
func (s *Store) WorkerReady(ctx context.Context, id string) (bool, error) {
	if err := s.requireManagement(); err != nil {
		return false, err
	}
	var ready bool
	err := s.pool.QueryRow(ctx, `SELECT NOT draining
		AND last_seen_at + heartbeat_ttl_ms * interval '1 millisecond' > clock_timestamp()
		FROM worker_nodes WHERE id=$1 AND role='runtime'`, id).Scan(&ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return ready, err
}

// Empty projectID authorizes the trusted system caller to control shared nodes.
// A project caller can only control nodes dedicated to its project.
func (s *Store) SetWorkerDraining(ctx context.Context, projectID, id, actor string, draining bool) (WorkerNode, error) {
	if err := s.requireManagement(); err != nil {
		return WorkerNode{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return WorkerNode{}, ErrConflict
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return WorkerNode{}, err
	}
	defer tx.Rollback(ctx)
	w, err := scanWorker(tx.QueryRow(ctx, `UPDATE worker_nodes SET draining=$3,updated_at=now()
		WHERE id=$1 AND ($2='' OR project_id=$2) RETURNING `+workerViewColumns, id, projectID, draining))
	if err != nil {
		return w, err
	}
	auditID, err := newID("audit")
	if err != nil {
		return w, err
	}
	action := "worker.enabled:" + id
	if draining {
		action = "worker.drained:" + id
	}
	if _, err := tx.Exec(ctx, `INSERT INTO management_audit(id,project_id,actor_id,action) VALUES($1,$2,$3,$4)`, auditID, projectID, actor, action); err != nil {
		return w, err
	}
	return w, tx.Commit(ctx)
}

type WorkerServices struct {
	Workers                    []WorkerNode         `json:"workers"`
	Runs                       map[string]int       `json:"runs"`
	Environments               map[string]int       `json:"environments"`
	ActiveLeases               int                  `json:"active_leases"`
	ExpiredLeases              int                  `json:"expired_leases"`
	LostAttempts               int                  `json:"lost_attempts"`
	PendingOutbox              int                  `json:"pending_outbox"`
	OldestOutboxSeconds        *float64             `json:"oldest_outbox_seconds"`
	DLQCount                   *int64               `json:"dlq_count"`
	DLQScope                   string               `json:"dlq_scope"`
	RecentRegistryAudit        []RegistryAuditEntry `json:"recent_registry_audit"`
	ServiceHealth              []ServiceHealth      `json:"service_health"`
	LeaseContention            *int64               `json:"lease_contention"`
	StaleFencingRejections     *int64               `json:"stale_fencing_rejections"`
	ServicesTelemetryAvailable bool                 `json:"services_telemetry_available"`
}

type RegistryAuditEntry struct {
	ID         int64     `json:"id"`
	ProjectID  string    `json:"project_id"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	ResourceID string    `json:"resource_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (s *Store) GetWorkerServices(ctx context.Context, projectID string) (WorkerServices, error) {
	view := WorkerServices{Workers: []WorkerNode{}, Runs: map[string]int{}, Environments: map[string]int{}, RecentRegistryAudit: []RegistryAuditEntry{},
		DLQScope: "project_retained_outbox_handoffs", ServiceHealth: []ServiceHealth{}}
	if err := s.requireManagement(); err != nil {
		return view, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+workerViewColumns+` FROM worker_nodes WHERE project_id IS NULL OR project_id=$1 ORDER BY role,id`, projectID)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			rows.Close()
			return view, err
		}
		view.Workers = append(view.Workers, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return view, err
	}
	for _, q := range []struct {
		sql string
		out map[string]int
	}{
		{`SELECT status,count(*) FROM runs WHERE project_id=$1 GROUP BY status`, view.Runs},
		{`SELECT state,count(*) FROM environments WHERE project_id=$1 GROUP BY state`, view.Environments},
	} {
		rows, err := s.pool.Query(ctx, q.sql, projectID)
		if err != nil {
			return view, err
		}
		for rows.Next() {
			var key string
			var n int
			if err := rows.Scan(&key, &n); err != nil {
				rows.Close()
				return view, err
			}
			q.out[key] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return view, err
		}
	}
	err = s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM environment_leases WHERE project_id=$1 AND state IN ('active','releasing') AND expires_at>clock_timestamp()),
		(SELECT count(*) FROM environment_leases WHERE project_id=$1 AND (state='expired' OR (state IN ('active','releasing') AND expires_at<=clock_timestamp()))),
		(SELECT count(*) FROM harness_attempts a JOIN runs r ON r.id=a.run_id WHERE r.project_id=$1 AND a.status='lost'),
		(SELECT count(*) FROM outbox_events o WHERE o.published_at IS NULL AND (o.key IN (SELECT id FROM runs WHERE project_id=$1) OR o.key IN (SELECT id FROM environments WHERE project_id=$1))),
		(SELECT extract(epoch FROM clock_timestamp()-min(o.created_at))::float8 FROM outbox_events o WHERE o.published_at IS NULL AND (o.key IN (SELECT id FROM runs WHERE project_id=$1) OR o.key IN (SELECT id FROM environments WHERE project_id=$1)))`,
		projectID).Scan(&view.ActiveLeases, &view.ExpiredLeases, &view.LostAttempts, &view.PendingOutbox, &view.OldestOutboxSeconds)
	if err != nil {
		return view, err
	}
	// Runtime/exporter DLQ handoffs have this envelope independent of the
	// deployment's topic name. Count retained rows, not Kafka queue depth.
	var dlqCount int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events o
		WHERE o.payload ? 'original' AND o.payload ? 'error'
		  AND (o.key IN (SELECT id FROM runs WHERE project_id=$1)
		    OR o.key IN (SELECT id FROM environments WHERE project_id=$1))`, projectID).Scan(&dlqCount); err != nil {
		return view, err
	}
	view.DLQCount = &dlqCount
	rows, err = s.pool.Query(ctx, `SELECT id,project_id,actor_id,action,resource_id,occurred_at
		FROM registry_audit WHERE project_id=$1
		  AND action IN ('skill.upload','skill.publish','skill.revoke','harness.publish','harness.revoke','mcp.bind','mcp.revoke')
		ORDER BY occurred_at DESC,id DESC LIMIT 20`, projectID)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var audit RegistryAuditEntry
		if err := rows.Scan(&audit.ID, &audit.ProjectID, &audit.ActorID, &audit.Action, &audit.ResourceID, &audit.OccurredAt); err != nil {
			return view, err
		}
		view.RecentRegistryAudit = append(view.RecentRegistryAudit, audit)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return view, err
	}
	err = s.readServicesTelemetry(ctx, projectID, &view)
	return view, err
}
