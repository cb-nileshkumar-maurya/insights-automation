package generation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	workerLimit       = 4
	executionDeadline = 45 * time.Second
	heartbeatInterval = 15 * time.Second
)

var sqlWorkerSlots = make(chan struct{}, workerLimit)

// SQLWorker executes durable child runs while sharing a four-query limit with
// every worker instance created by this process.
type SQLWorker struct {
	store    *SQLRunStore
	data     CricketData
	config   Configuration
	workerID string
	slots    chan struct{}
	now      func() time.Time
	deadline time.Duration
}

func NewSQLWorker(store *SQLRunStore, data CricketData, config Configuration, workerID string) *SQLWorker {
	return &SQLWorker{store: store, data: data, config: config, workerID: workerID, slots: sqlWorkerSlots, now: func() time.Time { return time.Now().UTC() }, deadline: executionDeadline}
}

// ProcessOne claims and processes at most one child run. A caller may invoke it
// concurrently; the worker's semaphore keeps at most four database-heavy runs active.
func (w *SQLWorker) ProcessOne(ctx context.Context) (bool, error) {
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	default:
		return false, nil
	}
	run, claimed, err := w.store.ClaimNext(ctx, w.workerID, w.now())
	if err != nil || !claimed {
		return claimed, err
	}
	template, ok := w.config.Templates[run.Target.Template]
	if !ok {
		return true, w.finishFailure(ctx, run, "", &RunFailure{Kind: ConfigurationFailure, Message: "unknown card template"})
	}
	hash, err := InputHash(run.Target.Template, template.Version, run.MatchID, run.CardState, run.NormalizedInputs)
	if err != nil {
		return true, w.finishFailure(ctx, run, "", &RunFailure{Kind: ValidationFailure, Message: err.Error()})
	}
	execution, cancel := context.WithTimeout(ctx, w.deadline)
	stopHeartbeat := w.heartbeat(execution, run.ID)
	generated := w.data.Generate(execution, GenerationQuery{Template: run.Target.Template, MatchID: run.MatchID, Inputs: canonicalInputs(run.NormalizedInputs)})
	timedOut := execution.Err() == context.DeadlineExceeded
	stopHeartbeat()
	cancel()
	if generated.Err != nil {
		return true, w.finishFailure(ctx, run, hash, generated.Err)
	}
	if timedOut {
		return true, w.finishFailure(ctx, run, hash, &RunFailure{Kind: TransientFailure, Message: "generation deadline exceeded"})
	}
	result := Result{Version: run.ResultVersion + 1, Envelope: ResultEnvelope{Template: run.Target.Template, TemplateVersion: template.Version, NormalizedFilters: canonicalInputs(run.NormalizedInputs), SourceDataWindow: generated.SourceDataWindow, SampleSize: generated.SampleSize, GeneratedAt: w.now(), ResultVersion: run.ResultVersion + 1, Fallbacks: generated.Fallbacks}, Data: generated.Data}
	if err := w.store.SaveResult(ctx, run, template.Version, hash, result); err != nil {
		return true, w.finishFailure(ctx, run, hash, &RunFailure{Kind: TransientFailure, Message: err.Error()})
	}
	return true, w.store.RefreshParentStatus(ctx, run.ParentID)
}

func (w *SQLWorker) heartbeat(ctx context.Context, runID string) func() {
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				_, _ = w.store.RenewLease(ctx, runID, w.workerID, w.now())
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}

func (w *SQLWorker) finishFailure(ctx context.Context, run Run, inputHash string, failure *RunFailure) error {
	if failure.Kind == TransientFailure && run.Attempt < 2 {
		return w.store.Requeue(ctx, run.ID, run.Attempt+1, w.now().Add(retryDelay(run.Attempt)), failure)
	}
	if err := w.store.Fail(ctx, run.ID, failure); err != nil {
		return err
	}
	return w.store.RefreshParentStatus(ctx, run.ParentID)
}

func retryDelay(attempt int) time.Duration { return time.Second << attempt }

func (w *SQLWorker) String() string { return fmt.Sprintf("SQLWorker(%s)", w.workerID) }
