CREATE TABLE control_sessions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  actor_id TEXT NOT NULL CHECK (length(trim(actor_id)) > 0),
  token_hash TEXT NOT NULL CHECK (length(token_hash) = 64),
  profile_generation BIGINT NOT NULL CHECK (profile_generation > 0),
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  state TEXT NOT NULL DEFAULT 'pending'
    CHECK (state IN ('pending', 'opening', 'leased', 'busy', 'closing', 'closed', 'needs_review')),
  provider_session_id TEXT NOT NULL DEFAULT '',
  operation_token TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_control_sessions_exclusive ON control_sessions(environment_id)
  WHERE state IN ('pending', 'opening', 'leased', 'busy', 'closing');
CREATE INDEX idx_control_sessions_expiry ON control_sessions(expires_at)
  WHERE state NOT IN ('closed', 'needs_review');

-- Identity survives environment cleanup for token replay refusal and audit.
-- Reset is the explicit recovery path for uncertain provider execution.
CREATE FUNCTION management_control_session_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.state IN ('resetting', 'deleting') AND NEW.state <> OLD.state) THEN
    IF EXISTS (SELECT 1 FROM control_sessions WHERE environment_id = OLD.id
      AND state IN ('pending', 'opening', 'leased', 'busy', 'closing')) THEN
      RAISE EXCEPTION 'environment has an active control session' USING ERRCODE = '23514';
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END
$$;
CREATE TRIGGER management_control_session_guard BEFORE UPDATE OR DELETE ON environments
  FOR EACH ROW EXECUTE FUNCTION management_control_session_guard();
