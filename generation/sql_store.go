package generation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLRunStore owns the project's write database. The application supplies a
// MariaDB-compatible sql.DB configured with write credentials.
type SQLRunStore struct {
	db      *sql.DB
	dialect DatabaseDialect
}

func NewSQLRunStoreWithDialect(db *sql.DB, dialect DatabaseDialect) (*SQLRunStore, error) {
	if db == nil {
		return nil, fmt.Errorf("write database is required")
	}
	return &SQLRunStore{db: db, dialect: dialect}, nil
}

func (s *SQLRunStore) Close() error { return s.db.Close() }

func (s *SQLRunStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *SQLRunStore) Migrate(ctx context.Context) error {
	if s.dialect != SQLite {
		return fmt.Errorf("automatic migration is only configured for SQLite")
	}
	_, err := s.db.ExecContext(ctx, sqliteSchema)
	if err != nil {
		return fmt.Errorf("migrate SQLite write database: %w", err)
	}
	return nil
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
	if s.dialect == SQLite {
		_, err = s.db.ExecContext(ctx, `
INSERT INTO generation_runs (
 id, parent_id, template_id, card_set_id, template_version, match_id, card_state,
 normalized_inputs, input_hash, mode, state, attempt, priority, result_version,
 failure_kind, failure_message, lease_until, available_at, created_at, updated_at
) VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
 state = excluded.state, attempt = excluded.attempt, result_version = excluded.result_version,
 failure_kind = excluded.failure_kind, failure_message = excluded.failure_message,
 lease_until = excluded.lease_until, updated_at = excluded.updated_at`,
			run.ID, run.ParentID, run.Target.Template, run.Target.CardSet, templateVersion,
			run.MatchID, run.CardState, inputs, inputHash, run.Mode, run.State, run.Attempt,
			priorityFor(run.Mode), run.ResultVersion, failureKind, failureMessage, nullableTime(run.LeaseUntil),
			run.CreatedAt, run.CreatedAt, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("save generation run: %w", err)
		}
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO generation_runs (
  id, parent_id, template_id, card_set_id, template_version, match_id, card_state,
  normalized_inputs, input_hash, mode, state, attempt, priority, result_version,
  failure_kind, failure_message, lease_until, available_at, created_at, updated_at
) VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, NULLIF(?, '0000-00-00 00:00:00'), ?, ?, ?)
ON DUPLICATE KEY UPDATE
  state = VALUES(state), attempt = VALUES(attempt), result_version = VALUES(result_version),
  failure_kind = VALUES(failure_kind), failure_message = VALUES(failure_message),
  lease_until = VALUES(lease_until), updated_at = VALUES(updated_at)`,
		run.ID, run.ParentID, run.Target.Template, run.Target.CardSet, templateVersion,
		run.MatchID, run.CardState, inputs, inputHash, run.Mode, run.State, run.Attempt,
		priorityFor(run.Mode), run.ResultVersion, failureKind, failureMessage, nullableTime(run.LeaseUntil),
		run.CreatedAt, run.CreatedAt, time.Now().UTC())
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
	currentResultStatement := `
INSERT INTO current_generation_results (
  template_id, template_version, match_id, card_state, input_hash, generation_result_id, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE generation_result_id = VALUES(generation_result_id), updated_at = VALUES(updated_at)`
	if s.dialect == SQLite {
		currentResultStatement = `
INSERT INTO current_generation_results (
  template_id, template_version, match_id, card_state, input_hash, generation_result_id, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(template_id, template_version, match_id, card_state, input_hash)
DO UPDATE SET generation_result_id = excluded.generation_result_id, updated_at = excluded.updated_at`
	}
	_, err = tx.ExecContext(ctx, currentResultStatement,
		run.Target.Template, templateVersion, run.MatchID, run.CardState, inputHash, resultID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update current generation result: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
UPDATE generation_runs
SET state = 'succeeded', result_version = ?, lease_owner = NULL, lease_until = NULL,
  updated_at = ?
WHERE id = ?`, result.Version, time.Now().UTC(), run.ID)
	if err != nil {
		return fmt.Errorf("complete generation run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit generation result: %w", err)
	}
	return nil
}

func (s *SQLRunStore) LoadRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, COALESCE(parent_id, ''), COALESCE(template_id, ''), COALESCE(card_set_id, ''),
  match_id, card_state, normalized_inputs, mode, state, attempt, COALESCE(result_version, 0),
  COALESCE(failure_kind, ''), COALESCE(failure_message, ''), lease_until, created_at
FROM generation_runs WHERE id = ?`, id)
	var run Run
	var inputs []byte
	var template, cardSet, state, mode, cardState, failureKind, failureMessage string
	var leaseUntil sql.NullTime
	if err := row.Scan(&run.ID, &run.ParentID, &template, &cardSet, &run.MatchID, &cardState, &inputs, &mode, &state, &run.Attempt, &run.ResultVersion, &failureKind, &failureMessage, &leaseUntil, &run.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("load generation run: %w", err)
	}
	if err := decodeNormalizedInputs(inputs, &run.NormalizedInputs); err != nil {
		return Run{}, fmt.Errorf("decode normalized inputs: %w", err)
	}
	run.Target = CardTarget{Template: TemplateID(template), CardSet: cardSet}
	run.CardState, run.Mode, run.State = CardState(cardState), RunMode(mode), RunState(state)
	if leaseUntil.Valid {
		run.LeaseUntil = leaseUntil.Time
	}
	if failureKind != "" {
		run.Failure = &RunFailure{Kind: FailureKind(failureKind), Message: failureMessage}
	}
	children, err := s.db.QueryContext(ctx, `SELECT id FROM generation_runs WHERE parent_id = ? ORDER BY created_at`, run.ID)
	if err != nil {
		return Run{}, fmt.Errorf("load child generation runs: %w", err)
	}
	defer children.Close()
	for children.Next() {
		var childID string
		if err := children.Scan(&childID); err != nil {
			return Run{}, fmt.Errorf("scan child generation run: %w", err)
		}
		run.Children = append(run.Children, childID)
	}
	if err := children.Err(); err != nil {
		return Run{}, fmt.Errorf("iterate child generation runs: %w", err)
	}
	return run, nil
}

func decodeNormalizedInputs(data []byte, inputs *map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(inputs); err != nil {
		return err
	}
	for key, value := range *inputs {
		normalized, err := normalizeInputJSON(value)
		if err != nil {
			return err
		}
		(*inputs)[key] = normalized
	}
	return nil
}

func normalizeInputJSON(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		integer, err := value.Int64()
		if err != nil {
			return nil, err
		}
		return int(integer), nil
	case []any:
		values := make([]any, len(value))
		strings := make([]string, len(value))
		allStrings := true
		for index, item := range value {
			normalized, err := normalizeInputJSON(item)
			if err != nil {
				return nil, err
			}
			values[index] = normalized
			stringValue, ok := normalized.(string)
			if !ok {
				allStrings = false
				continue
			}
			strings[index] = stringValue
		}
		if allStrings {
			return strings, nil
		}
		return values, nil
	default:
		return value, nil
	}
}

// FindActiveRun returns matching normal work so concurrent reconciliation
// passes join the original request instead of creating duplicate children.
func (s *SQLRunStore) FindActiveRun(ctx context.Context, target CardTarget, matchID string, cardState CardState, inputHash string) (Run, error) {
	column, targetID := "template_id", string(target.Template)
	if target.CardSet != "" {
		column, targetID = "card_set_id", target.CardSet
	}
	row := s.db.QueryRowContext(ctx, `SELECT id FROM generation_runs WHERE `+column+` = ? AND match_id = ? AND card_state = ? AND input_hash = ? AND mode = 'normal' AND state IN ('queued', 'running') ORDER BY created_at LIMIT 1`, targetID, matchID, cardState, inputHash)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("find active generation run: %w", err)
	}
	return s.LoadRun(ctx, id)
}

func (s *SQLRunStore) LoadCurrentResult(ctx context.Context, template TemplateID, templateVersion, matchID string, cardState CardState, inputHash string) (Result, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT r.result_version, r.normalized_inputs, r.source_data_window, r.sample_size,
  r.fallbacks, r.result_data, r.generated_at
FROM current_generation_results c
JOIN generation_results r ON r.id = c.generation_result_id
WHERE c.template_id = ? AND c.template_version = ? AND c.match_id = ?
  AND c.card_state = ? AND c.input_hash = ?`, template, templateVersion, matchID, cardState, inputHash)
	var result Result
	var filters, fallbacks, data []byte
	if err := row.Scan(&result.Version, &filters, &result.Envelope.SourceDataWindow, &result.Envelope.SampleSize, &fallbacks, &data, &result.Envelope.GeneratedAt); err != nil {
		if err == sql.ErrNoRows {
			return Result{}, ErrNotFound
		}
		return Result{}, fmt.Errorf("load current generation result: %w", err)
	}
	if err := json.Unmarshal(filters, &result.Envelope.NormalizedFilters); err != nil {
		return Result{}, fmt.Errorf("decode normalized filters: %w", err)
	}
	if err := json.Unmarshal(fallbacks, &result.Envelope.Fallbacks); err != nil {
		return Result{}, fmt.Errorf("decode fallbacks: %w", err)
	}
	if err := json.Unmarshal(data, &result.Data); err != nil {
		return Result{}, fmt.Errorf("decode result data: %w", err)
	}
	result.Envelope.Template = template
	result.Envelope.TemplateVersion = templateVersion
	result.Envelope.ResultVersion = result.Version
	return result, nil
}

func (s *SQLRunStore) LoadCurrentResultByLocator(ctx context.Context, locator string) (Result, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT c.template_id, c.template_version, r.result_version, r.normalized_inputs,
  r.source_data_window, r.sample_size, r.fallbacks, r.result_data, r.generated_at
FROM current_generation_results c
JOIN generation_results r ON r.id = c.generation_result_id
WHERE c.input_hash = ?`, locator)
	var result Result
	var template, templateVersion string
	var filters, fallbacks, data []byte
	if err := row.Scan(&template, &templateVersion, &result.Version, &filters, &result.Envelope.SourceDataWindow, &result.Envelope.SampleSize, &fallbacks, &data, &result.Envelope.GeneratedAt); err != nil {
		if err == sql.ErrNoRows {
			return Result{}, ErrNotFound
		}
		return Result{}, fmt.Errorf("load current result by locator: %w", err)
	}
	if err := json.Unmarshal(filters, &result.Envelope.NormalizedFilters); err != nil {
		return Result{}, fmt.Errorf("decode normalized filters: %w", err)
	}
	if err := json.Unmarshal(fallbacks, &result.Envelope.Fallbacks); err != nil {
		return Result{}, fmt.Errorf("decode fallbacks: %w", err)
	}
	if err := json.Unmarshal(data, &result.Data); err != nil {
		return Result{}, fmt.Errorf("decode result data: %w", err)
	}
	result.Envelope.Template = TemplateID(template)
	result.Envelope.TemplateVersion = templateVersion
	result.Envelope.ResultVersion = result.Version
	return result, nil
}

func (s *SQLRunStore) LoadLatestRunByLocator(ctx context.Context, locator string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM generation_runs WHERE template_id IS NOT NULL AND input_hash = ? ORDER BY created_at DESC LIMIT 1`, locator)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("load latest generation run by locator: %w", err)
	}
	return s.LoadRun(ctx, id)
}

// ClaimNext leases one queued child run. Manual regenerations sort before normal
// work through the persisted priority column, but all work uses the same lease.
func (s *SQLRunStore) ClaimNext(ctx context.Context, workerID string, now time.Time) (Run, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, fmt.Errorf("begin run claim: %w", err)
	}
	defer tx.Rollback()
	var id string
	claimStatement := `
SELECT id FROM generation_runs
WHERE state = 'queued' AND available_at <= ? AND template_id IS NOT NULL
ORDER BY priority DESC, created_at ASC
LIMIT 1 FOR UPDATE SKIP LOCKED`
	if s.dialect == SQLite {
		claimStatement = `
SELECT id FROM generation_runs
WHERE state = 'queued' AND available_at <= ? AND template_id IS NOT NULL
ORDER BY priority DESC, created_at ASC LIMIT 1`
	}
	err = tx.QueryRowContext(ctx, claimStatement, now).Scan(&id)
	if err == sql.ErrNoRows {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, fmt.Errorf("find queued generation run: %w", err)
	}
	leaseUntil := now.Add(60 * time.Second)
	if _, err := tx.ExecContext(ctx, `
UPDATE generation_runs
SET state = 'running', lease_owner = ?, lease_until = ?, updated_at = ?
WHERE id = ?`, workerID, leaseUntil, now, id); err != nil {
		return Run{}, false, fmt.Errorf("lease generation run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Run{}, false, fmt.Errorf("commit run claim: %w", err)
	}
	run, err := s.LoadRun(ctx, id)
	if err != nil {
		return Run{}, false, err
	}
	return run, true, nil
}

func (s *SQLRunStore) RenewLease(ctx context.Context, runID, workerID string, now time.Time) (bool, error) {
	updated, err := s.db.ExecContext(ctx, `
UPDATE generation_runs
SET lease_until = ?, updated_at = ?
WHERE id = ? AND state = 'running' AND lease_owner = ?`, now.Add(60*time.Second), now, runID, workerID)
	if err != nil {
		return false, fmt.Errorf("renew generation lease: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read renewed lease count: %w", err)
	}
	return count == 1, nil
}

func (s *SQLRunStore) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	updated, err := s.db.ExecContext(ctx, `
UPDATE generation_runs
SET state = 'queued', lease_owner = NULL, lease_until = NULL, updated_at = ?
WHERE state = 'running' AND lease_until < ?`, now, now)
	if err != nil {
		return 0, fmt.Errorf("recover expired generation leases: %w", err)
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read recovered lease count: %w", err)
	}
	return count, nil
}

func (s *SQLRunStore) Requeue(ctx context.Context, runID string, attempt int, availableAt time.Time, failure *RunFailure) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE generation_runs
SET state = 'queued', attempt = ?, failure_kind = ?, failure_message = ?,
  lease_owner = NULL, lease_until = NULL, available_at = ?, updated_at = ?
WHERE id = ?`, attempt, failure.Kind, failure.Message, availableAt, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("requeue generation run: %w", err)
	}
	return nil
}

func (s *SQLRunStore) Fail(ctx context.Context, runID string, failure *RunFailure) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE generation_runs
SET state = 'failed', failure_kind = ?, failure_message = ?,
  lease_owner = NULL, lease_until = NULL, updated_at = ?
WHERE id = ?`, failure.Kind, failure.Message, time.Now().UTC(), runID)
	if err != nil {
		return fmt.Errorf("fail generation run: %w", err)
	}
	return nil
}

func (s *SQLRunStore) RefreshParentStatus(ctx context.Context, parentID string) error {
	if parentID == "" {
		return nil
	}
	var total, succeeded, failed int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), SUM(state = 'succeeded'), SUM(state = 'failed')
FROM generation_runs WHERE parent_id = ?`, parentID).Scan(&total, &succeeded, &failed)
	if err != nil {
		return fmt.Errorf("count child generation runs: %w", err)
	}
	if total == 0 || succeeded+failed != total {
		return nil
	}
	state := Succeeded
	if failed == total {
		state = Failed
	} else if failed > 0 {
		state = CompletedWithErrors
	}
	_, err = s.db.ExecContext(ctx, `UPDATE generation_runs SET state = ?, updated_at = ? WHERE id = ?`, state, time.Now().UTC(), parentID)
	if err != nil {
		return fmt.Errorf("update parent generation run: %w", err)
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
