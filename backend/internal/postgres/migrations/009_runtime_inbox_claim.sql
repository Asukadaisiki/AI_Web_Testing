ALTER TABLE inbox_events
  ADD COLUMN claim_token TEXT,
  ADD COLUMN claimed_until TIMESTAMPTZ,
  ADD CONSTRAINT inbox_claim_pair CHECK (
    (claim_token IS NULL) = (claimed_until IS NULL)
  );

CREATE INDEX idx_inbox_abandoned_claims
  ON inbox_events (claimed_until)
  WHERE processed_at IS NULL AND claim_token IS NOT NULL;
