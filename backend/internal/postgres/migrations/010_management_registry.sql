CREATE TABLE skills (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  name TEXT NOT NULL,
  slug TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(project_id,slug)
);
CREATE TABLE skill_versions (
  id TEXT PRIMARY KEY,
  skill_id TEXT NOT NULL REFERENCES skills(id),
  version INTEGER NOT NULL CHECK(version>0),
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','revoked')),
  files JSONB NOT NULL CHECK(jsonb_typeof(files)='array'),
  content_digest TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ,
  UNIQUE(skill_id,version)
);
CREATE TABLE mcp_definitions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  owner_scope TEXT NOT NULL DEFAULT 'system' CHECK(owner_scope='system'),
  status TEXT NOT NULL DEFAULT 'active'
);
CREATE TABLE mcp_versions (
  id TEXT PRIMARY KEY,
  mcp_definition_id TEXT NOT NULL REFERENCES mcp_definitions(id),
  version INTEGER NOT NULL CHECK(version>0),
  descriptor JSONB NOT NULL CHECK(jsonb_typeof(descriptor)='object'),
  content_digest TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ,
  UNIQUE(mcp_definition_id,version)
);
CREATE TABLE project_mcp_bindings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  mcp_version_id TEXT NOT NULL REFERENCES mcp_versions(id),
  config JSONB NOT NULL DEFAULT '{}',
  secret_refs JSONB NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT true,
  revision INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE project_harnesses (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  name TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);
CREATE TABLE harness_versions (
  id TEXT PRIMARY KEY,
  harness_id TEXT NOT NULL REFERENCES project_harnesses(id),
  version INTEGER NOT NULL CHECK(version>0),
  manifest JSONB NOT NULL,
  snapshot JSONB NOT NULL,
  content_digest TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ,
  UNIQUE(harness_id,version)
);
CREATE TABLE registry_audit (
  id BIGSERIAL PRIMARY KEY,
  project_id TEXT,
  actor_id TEXT NOT NULL,
  action TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE FUNCTION protect_registry_content() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'registry versions are immutable; revoke instead';
  END IF;
  IF TG_TABLE_NAME='skill_versions' THEN
    IF OLD.skill_id<>NEW.skill_id OR OLD.version<>NEW.version OR OLD.files<>NEW.files OR OLD.content_digest<>NEW.content_digest OR OLD.created_by<>NEW.created_by OR OLD.created_at<>NEW.created_at THEN
      RAISE EXCEPTION 'skill version content is immutable';
    END IF;
    IF OLD.status='revoked' AND NEW.status<>'revoked' OR OLD.status='published' AND NEW.status='draft' THEN
      RAISE EXCEPTION 'invalid skill publication transition';
    END IF;
  ELSE
    IF (to_jsonb(OLD)-'revoked_at')<>(to_jsonb(NEW)-'revoked_at') THEN
      RAISE EXCEPTION 'registry version content is immutable';
    END IF;
  END IF;
  IF OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
    RAISE EXCEPTION 'revocation is permanent';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER skill_content_immutable BEFORE UPDATE OR DELETE ON skill_versions FOR EACH ROW EXECUTE FUNCTION protect_registry_content();
CREATE TRIGGER mcp_content_immutable BEFORE UPDATE OR DELETE ON mcp_versions FOR EACH ROW EXECUTE FUNCTION protect_registry_content();
CREATE TRIGGER harness_content_immutable BEFORE UPDATE OR DELETE ON harness_versions FOR EACH ROW EXECUTE FUNCTION protect_registry_content();
