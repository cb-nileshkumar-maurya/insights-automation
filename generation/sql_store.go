package generation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLRunStore owns the project's write database. The application supplies a
// MariaDB-compatible sql.DB configured with write credentials.
type SQLRunStore struct {
	db *sql.DB
}

func NewSQLRunStore(db *sql.DB) (*SQLRunStore, error) {
	if db == nil {
		return nil, fmt.Errorf("write database is required")
	}
	return &SQLRunStore{db: db}, nil
}

func (s *SQLRunStore) SaveRun(ctx context.Context, run Run, templateVersion string, inputHash string) error {
	inputs, err := json.Marshal(run.NormalizedInputs)
	if err != nil {
		return fmt.Errorf("marshal normalized inputs: %w", err)
	}
	var failureKind, failureMessage any
	if run.Failure != nil {
		failureKind, failureMessage = run.Failure.Kind, run.Failure.Message
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO generation_runs (
  id, parent_id, template_id, card_set_id, template_version, match_id, card_state,
  normalized_inputs, input_hash, mode, state, attempt, priority, result_version,
  failure_kind, failure_message, lease_until, created_at, updated_at
) VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, NULLIF(?, '0000-00-00 00:00:00'), ?, ?)
ON DUPLICATE KEY UPDATE
  state = VALUES(state), attempt = VALUES(attempt), result_version = VALUES(result_version),
  failure_kind = VALUES(failure_kind), failure_message = VALUES(failure_message),
  lease_until = VALUES(lease_until), updated_at = VALUES(updated_at)`,
		run.ID, run.ParentID, run.Target.Template, run.Target.CardSet, templateVersion,
		run.MatchID, run.CardState, inputs, inputHash, run.Mode, run.State, run.Attempt,
		priorityFor(run.Mode), run.ResultVersion, failureKind, failureMessage, nullableTime(run.LeaseUntil),
		run.CreatedAt, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("save generation run: %w", err)
	}
	return nil
}

func (s *SQLRunStore) SaveResult(ctx context.Context, run Run, templateVersion string, inputHash string, result Result) error {
	filters, err := json.Marshal(result.Envelope.NormalizedFilters)
	if err != nil {
		return fmt.Errorf("marshal normalized filters: %w", err)
	}
	fallbacks, err := json.Marshal(result.Envelope.Fallbacks)
	if err != nil {
		return fmt.Errorf("marshal fallbacks: %w", err)
	}
	data, err := json.Marshal(result.Data)
	if err != nil {
		return fmt.Errorf("marshal result data: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin result transaction: %w", err)
	}
	defer tx.Rollback()
	stored, err := tx.ExecContext(ctx, `
INSERT INTO generation_results (
  run_id, template_id, template_version, match_id, card_state, normalized_inputs,
  input_hash, result_version, source_data_window, sample_size, fallbacks, result_data, generated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Target.Template, templateVersion, run.MatchID, run.CardState, filters,
		inputHash, result.Version, result.Envelope.SourceDataWindow, result.Envelope.SampleSize,
		fallbacks, data, result.Envelope.GeneratedAt)
	if err != nil {
		return fmt.Errorf("save generation result: %w", err)
	}
	resultID, err := stored.LastInsertId()
	if err != nil {
		return fmt.Errorf("read generation result id: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO current_generation_results (
  template_id, template_version, match_id, card_state, input_hash, generation_result_id, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE generation_result_id = VALUES(generation_result_id), updated_at = VALUES(updated_at)`,
		run.Target.Template, templateVersion, run.MatchID, run.CardState, inputHash, resultID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update current generation result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit generation result: %w", err)
	}
	return nil
}

func priorityFor(mode RunMode) int {
	if mode == Regeneration {
		return 1
	}
	return 0
}
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
