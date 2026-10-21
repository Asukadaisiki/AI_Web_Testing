CREATE TABLE consumer_retry_counts (
  consumer TEXT NOT NULL,
  event_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (consumer, event_id)
);
