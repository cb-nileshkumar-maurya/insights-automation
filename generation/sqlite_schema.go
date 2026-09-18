package generation

const sqliteSchema = `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS generation_runs (
 generation_run_id TEXT PRIMARY KEY, parent_generation_run_id TEXT, template_id TEXT, card_set_id TEXT, template_version TEXT,
 match_id TEXT NOT NULL, card_state TEXT NOT NULL, normalized_inputs TEXT NOT NULL, result_locator TEXT NOT NULL,
 mode TEXT NOT NULL, state TEXT NOT NULL, attempt INTEGER NOT NULL DEFAULT 0, priority INTEGER NOT NULL DEFAULT 0,
 result_version INTEGER, failure_kind TEXT, failure_message TEXT, lease_owner TEXT, lease_until DATETIME,
 available_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 CHECK ((template_id IS NULL) != (card_set_id IS NULL)),
 FOREIGN KEY (parent_generation_run_id) REFERENCES generation_runs(generation_run_id)
);
CREATE INDEX IF NOT EXISTS generation_runs_claim ON generation_runs(state, available_at, priority, created_at);
CREATE INDEX IF NOT EXISTS generation_runs_active ON generation_runs(template_id, template_version, match_id, card_state, result_locator, state);
CREATE INDEX IF NOT EXISTS generation_runs_parent ON generation_runs(parent_generation_run_id);
CREATE TABLE IF NOT EXISTS generation_results (
 generation_result_id INTEGER PRIMARY KEY AUTOINCREMENT, generation_run_id TEXT NOT NULL, template_id TEXT NOT NULL, template_version TEXT NOT NULL,
 match_id TEXT NOT NULL, card_state TEXT NOT NULL, normalized_inputs TEXT NOT NULL, result_locator TEXT NOT NULL,
 result_version INTEGER NOT NULL, source_data_window TEXT NOT NULL, sample_size INTEGER NOT NULL,
 fallbacks TEXT NOT NULL, result_data TEXT NOT NULL, generated_at DATETIME NOT NULL,
 UNIQUE (template_id, template_version, match_id, card_state, result_locator, result_version),
 FOREIGN KEY (generation_run_id) REFERENCES generation_runs(generation_run_id)
);
CREATE TABLE IF NOT EXISTS generation_result_pointers (
 template_id TEXT NOT NULL, template_version TEXT NOT NULL, match_id TEXT NOT NULL, card_state TEXT NOT NULL,
 result_locator TEXT NOT NULL, generation_result_id INTEGER NOT NULL, updated_at DATETIME NOT NULL,
 PRIMARY KEY (template_id, template_version, match_id, card_state, result_locator),
 FOREIGN KEY (generation_result_id) REFERENCES generation_results(generation_result_id)
);`
