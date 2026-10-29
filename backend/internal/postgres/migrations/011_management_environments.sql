CREATE TABLE project_bindings (
  agenttrace_project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE RESTRICT,
  langfuse_project_id TEXT NOT NULL UNIQUE CHECK (length(trim(langfuse_project_id)) > 0),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleting')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION management_immutable_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'project binding identity is immutable';
  END IF;
  IF NEW.agenttrace_project_id <> OLD.agenttrace_project_id OR
     NEW.langfuse_project_id <> OLD.langfuse_project_id THEN
    RAISE EXCEPTION 'project binding identity is immutable';
  END IF;
  RETURN NEW;
END
$$;
CREATE TRIGGER management_immutable_binding BEFORE UPDATE OR DELETE ON project_bindings
  FOR EACH ROW EXECUTE FUNCTION management_immutable_binding();

ALTER TABLE conversations
  ADD COLUMN state TEXT NOT NULL DEFAULT 'active'
    CHECK (state IN ('active', 'archived', 'deleting', 'deleted')),
  ADD COLUMN deleted_at TIMESTAMPTZ,
  ADD CONSTRAINT conversations_project_identity UNIQUE (id, project_id);

CREATE TABLE environments (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  conversation_id TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL DEFAULT 'provisioning'
    CHECK (state IN ('provisioning', 'ready', 'leased', 'resetting', 'needs_review', 'deleting', 'broken')),
  profile_generation BIGINT NOT NULL DEFAULT 1 CHECK (profile_generation > 0),
  fencing_token BIGINT NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
  policy JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(policy) = 'object'),
  profile_ref TEXT,
  last_used_at TIMESTAMPTZ,
  created_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  FOREIGN KEY (conversation_id, project_id) REFERENCES conversations(id, project_id) ON DELETE RESTRICT
);

-- Identity is retained after profile cleanup for command replay and audit.
CREATE TABLE environment_commands (
  event_id TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL,
  conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('provision', 'reset', 'delete')),
  profile_generation BIGINT NOT NULL CHECK (profile_generation > 0),
  payload_hash TEXT NOT NULL,
  command_hash TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'succeeded')),
  claim_token TEXT,
  claimed_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_environment_commands_pending ON environment_commands(environment_id)
  WHERE state <> 'succeeded';

CREATE TABLE management_audit (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  conversation_id TEXT,
  environment_id TEXT,
  actor_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
