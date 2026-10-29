ALTER TABLE environments ADD COLUMN browser_worker_id TEXT REFERENCES worker_nodes(id) ON DELETE RESTRICT;
CREATE INDEX idx_environment_browser_worker ON environments(browser_worker_id);
