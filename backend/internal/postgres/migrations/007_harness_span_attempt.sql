-- Harness source sequences restart for each subprocess attempt. Persist the
-- attempt identity on emitted spans so later evidence cannot link across
-- retries that reuse the same source sequence.
ALTER TABLE spans
  ADD COLUMN harness_attempt_id TEXT REFERENCES harness_attempts(id) ON DELETE SET NULL;

CREATE INDEX idx_spans_harness_attempt_source_sequence
  ON spans (harness_attempt_id, source_sequence);
