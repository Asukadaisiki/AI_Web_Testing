CREATE TABLE worker_nodes (
  id TEXT PRIMARY KEY,
  role TEXT NOT NULL CHECK (role IN ('runtime', 'browser')),
  project_id TEXT REFERENCES projects(id) ON DELETE RESTRICT,
  endpoint TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  runtime_ids JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(runtime_ids) = 'array'),
  capacity INTEGER NOT NULL CHECK (capacity > 0),
  in_use INTEGER NOT NULL DEFAULT 0 CHECK (in_use >= 0 AND in_use <= capacity),
  draining BOOLEAN NOT NULL DEFAULT false,
  heartbeat_ttl_ms BIGINT NOT NULL DEFAULT 30000 CHECK (heartbeat_ttl_ms > 0),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE harness_attempts ADD COLUMN sdk_started_at TIMESTAMPTZ;

CREATE TABLE worker_assignments (
  attempt_id TEXT PRIMARY KEY REFERENCES harness_attempts(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  runtime_worker_id TEXT NOT NULL REFERENCES worker_nodes(id) ON DELETE RESTRICT,
  browser_worker_id TEXT NOT NULL REFERENCES worker_nodes(id) ON DELETE RESTRICT,
  profile_generation BIGINT NOT NULL CHECK (profile_generation > 0),
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'releasing', 'released', 'expired')),
  assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  released_at TIMESTAMPTZ
);
CREATE INDEX idx_assignments_project ON worker_assignments(project_id, state);

CREATE TABLE environment_leases (
  environment_id TEXT PRIMARY KEY REFERENCES environments(id) ON DELETE CASCADE,
  attempt_id TEXT NOT NULL UNIQUE REFERENCES harness_attempts(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  runtime_worker_id TEXT NOT NULL REFERENCES worker_nodes(id) ON DELETE RESTRICT,
  browser_worker_id TEXT NOT NULL REFERENCES worker_nodes(id) ON DELETE RESTRICT,
  profile_generation BIGINT NOT NULL CHECK (profile_generation > 0),
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  renewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'releasing', 'released', 'expired'))
);
CREATE INDEX idx_leases_expiration ON environment_leases(expires_at) WHERE state IN ('active', 'releasing');
