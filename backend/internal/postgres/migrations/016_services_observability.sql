CREATE TABLE service_heartbeats (
  role TEXT NOT NULL CHECK (role IN ('api','outbox_relay','exporter')),
  instance_id TEXT NOT NULL,
  project_id TEXT NOT NULL DEFAULT '',
  healthy BOOLEAN NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  last_healthy_at TIMESTAMPTZ,
  PRIMARY KEY(role,instance_id,project_id)
);

CREATE TABLE service_counters (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (name IN ('lease_contention','stale_fencing_rejections')),
  value BIGINT NOT NULL CHECK (value > 0),
  first_observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  last_observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(project_id,name)
);
