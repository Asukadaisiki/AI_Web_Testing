-- Harness attempts give each subprocess execution of a run its own durable
-- identity so a crash + Kafka redelivery can allocate a fresh attempt without
-- overwriting prior evidence.
CREATE TABLE harness_attempts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  attempt_number BIGINT NOT NULL,
  provider_attempt_id TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (run_id, attempt_number)
);

-- Source identity columns let AppendHarnessEvent enforce exact next sequence
-- per harness attempt and detect same-identity/different-payload conflicts.
ALTER TABLE run_events
  ADD COLUMN harness_attempt_id TEXT REFERENCES harness_attempts(id) ON DELETE SET NULL,
  ADD COLUMN source_sequence BIGINT,
  ADD COLUMN source_type TEXT;

-- Each harness attempt produces at most one durable event per source sequence.
CREATE UNIQUE INDEX uq_run_events_harness_source
  ON run_events (run_id, harness_attempt_id, source_sequence)
  WHERE harness_attempt_id IS NOT NULL;

CREATE INDEX idx_run_events_harness_attempt
  ON run_events (harness_attempt_id, source_sequence);
