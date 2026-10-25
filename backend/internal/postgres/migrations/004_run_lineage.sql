ALTER TABLE runs
  ADD COLUMN source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
  ADD COLUMN retry_of_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL;

UPDATE runs AS run
SET source_message_id = message.id
FROM messages AS message
WHERE message.run_id = run.id
  AND message.role = 'user'
  AND run.source_message_id IS NULL;

CREATE INDEX idx_runs_conversation_created
  ON runs (conversation_id, created_at, id);

CREATE INDEX idx_runs_source_message
  ON runs (source_message_id, created_at, id);
