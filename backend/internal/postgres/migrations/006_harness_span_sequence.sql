-- Harness spans carry their source sequence so LatestModelSpanID and
-- ActiveToolSpanID can order spans deterministically by emission order
-- instead of relying on started_at, which is unsafe under clock skew or
-- fast consecutive events.
ALTER TABLE spans
  ADD COLUMN source_sequence BIGINT;

CREATE INDEX idx_spans_run_kind_source_sequence
  ON spans (run_id, kind, source_sequence);
