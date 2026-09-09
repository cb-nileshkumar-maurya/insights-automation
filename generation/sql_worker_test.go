package generation

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openWorkerTestStore(t *testing.T) *SQLRunStore {
	t.Helper()
	store, err := OpenSQLRunStore(context.Background(), DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "worker.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func workerRequest(mode RunMode) StartRequest {
	return StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Mode: mode, Caller: Caller{Role: Operations}, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}}
}

func TestSQLWorkerRetriesTransientFailureThenFailsPermanently(t *testing.T) {
	ctx, store, now := context.Background(), openWorkerTestStore(t), ParseTime("2026-09-09T00:00:00Z")
	module := NewSQLModule(DefaultConfiguration(), store, ClockFunc(func() Time { return now }))
	run, err := module.StartRun(ctx, workerRequest(Normal))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Err: &RunFailure{Kind: TransientFailure, Message: "replica unavailable"}}}}, DefaultConfiguration(), "worker")
	worker.now = func() time.Time { return now }
	for attempt := 0; attempt < 2; attempt++ {
		if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
			t.Fatalf("attempt %d claimed=%v err=%v", attempt, claimed, err)
		}
		now = now.Add(retryDelay(attempt))
	}
	if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("final claimed=%v err=%v", claimed, err)
	}
	failed, err := module.GetRun(ctx, run.ID)
	if err != nil || failed.State != Failed || failed.Attempt != 2 {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
}

func TestSQLWorkerDoesNotRetryPermanentFailure(t *testing.T) {
	ctx, store := context.Background(), openWorkerTestStore(t)
	module := NewSQLModule(DefaultConfiguration(), store, ClockFunc(func() Time { return ParseTime("2026-09-09T00:00:00Z") }))
	run, err := module.StartRun(ctx, workerRequest(Normal))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Err: &RunFailure{Kind: ConfigurationFailure, Message: "bad template"}}}}, DefaultConfiguration(), "worker")
	if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	failed, _ := module.GetRun(ctx, run.ID)
	if failed.State != Failed || failed.Attempt != 0 {
		t.Fatalf("failed=%#v", failed)
	}
}

func TestSQLStoreRecoversExpiredLeaseAndManualWorkTakesPriority(t *testing.T) {
	ctx, store, now := context.Background(), openWorkerTestStore(t), ParseTime("2026-09-09T00:00:00Z")
	module := NewSQLModule(DefaultConfiguration(), store, ClockFunc(func() Time { return now }))
	normal, err := module.StartRun(ctx, workerRequest(Normal))
	if err != nil {
		t.Fatal(err)
	}
	manual, err := module.StartRun(ctx, workerRequest(Regeneration))
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNext(ctx, "worker", now)
	if err != nil || !ok || claimed.ID != manual.ID {
		t.Fatalf("claimed=%#v ok=%v err=%v", claimed, ok, err)
	}
	if recovered, err := store.RecoverExpiredLeases(ctx, claimed.LeaseUntil.Add(time.Nanosecond)); err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	claimed, ok, err = store.ClaimNext(ctx, "worker", claimed.LeaseUntil.Add(time.Second))
	if err != nil || !ok || claimed.ID != manual.ID || normal.ID == manual.ID {
		t.Fatalf("reclaimed=%#v ok=%v err=%v", claimed, ok, err)
	}
}

type deadlineData struct{}

func (deadlineData) Generate(ctx context.Context, _ GenerationQuery) GeneratedData {
	<-ctx.Done()
	return GeneratedData{}
}

func TestSQLWorkerRequeuesDeadlineFailureAndHonorsWorkerLimit(t *testing.T) {
	ctx, store, now := context.Background(), openWorkerTestStore(t), ParseTime("2026-09-09T00:00:00Z")
	module := NewSQLModule(DefaultConfiguration(), store, ClockFunc(func() Time { return now }))
	run, err := module.StartRun(ctx, workerRequest(Normal))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorker(store, deadlineData{}, DefaultConfiguration(), "worker")
	worker.now, worker.deadline = func() time.Time { return now }, time.Millisecond
	if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("deadline claimed=%v err=%v", claimed, err)
	}
	requeued, _ := module.GetRun(ctx, run.ID)
	if requeued.State != Queued || requeued.Attempt != 1 || requeued.Failure.Kind != TransientFailure {
		t.Fatalf("requeued=%#v", requeued)
	}
	limited := NewSQLWorker(store, deadlineData{}, DefaultConfiguration(), "limited")
	limited.slots = make(chan struct{}, 1)
	limited.slots <- struct{}{}
	if claimed, err := limited.ProcessOne(ctx); err != nil || claimed {
		t.Fatalf("limited claimed=%v err=%v", claimed, err)
	}
}
