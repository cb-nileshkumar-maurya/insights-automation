package tests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	. "github.com/cricbuzz/insights-automation/generation"
)

type generationHarness struct {
	ctx    context.Context
	module *SQLModule
	worker *SQLWorker
}

func newGenerationHarness(t *testing.T, data CricketData) generationHarness {
	t.Helper()
	store := openWorkerTestStore(t)
	config := DefaultConfiguration()
	return generationHarness{
		ctx:    context.Background(),
		module: NewSQLModule(config, store, ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") })),
		worker: NewSQLWorker(store, data, config, "test-worker"),
	}
}

func (h generationHarness) startAndProcess(t *testing.T, request StartRequest) Run {
	t.Helper()
	run, err := h.module.StartRun(h.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := h.worker.ProcessOne(h.ctx); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	return run
}

func TestSQLModuleKeepsCurrentResultAfterModuleRestart(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLRunStore(ctx, DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := ParseTime("2026-09-08T10:00:00Z")
	config := DefaultConfiguration()
	module := NewSQLModule(config, store, ClockFunc(func() Time { return now }))
	request := StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"}}
	run, err := module.StartRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{"wins": 3}, SampleSize: 3, SourceDataWindow: "2019-01-01..2026-01-01"}}}, config, "test-worker")
	if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	freshModule := NewSQLModule(config, store, ClockFunc(func() Time { return now }))
	result, err := freshModule.GetCurrentResult(ctx, H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Version != 1 || result.Data["wins"] != float64(3) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	locators, err := freshModule.ResultLocators(run)
	if err != nil {
		t.Fatal(err)
	}
	located, err := freshModule.GetResult(ctx, locators[H2HRecord])
	if err != nil || located.ResultStatus.Status != Succeeded || located.Result == nil || located.Result.Version != 1 {
		t.Fatalf("located=%#v err=%v", located, err)
	}
	request.Mode = Regeneration
	request.Caller = Caller{Role: Editor}
	failedRun, err := freshModule.StartRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	located, err = freshModule.GetResult(ctx, locators[H2HRecord])
	if err != nil || located.ResultStatus.Status != Succeeded || located.Result == nil || located.Result.Version != 1 {
		t.Fatalf("located during regeneration=%#v err=%v", located, err)
	}
	failingWorker := NewSQLWorker(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Err: &RunFailure{Kind: ConfigurationFailure, Message: "invalid template"}}}}, config, "failing-worker")
	if claimed, err := failingWorker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	failed, err := freshModule.GetRun(ctx, failedRun.ID)
	if err != nil || failed.State != Failed {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
	current, err := freshModule.GetCurrentResult(ctx, H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || current.Version != 1 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	located, err = freshModule.GetResult(ctx, locators[H2HRecord])
	if err != nil || located.ResultStatus.Status != Succeeded || located.Result == nil || located.Result.Version != 1 || located.Failure != nil {
		t.Fatalf("located after failed regeneration=%#v err=%v", located, err)
	}
}

func TestSQLModuleRegenerationCreatesNextResultVersion(t *testing.T) {
	harness := newGenerationHarness(t, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{"wins": 3}, SampleSize: 3}}})
	request := StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"}}
	harness.startAndProcess(t, request)
	request.Mode = Regeneration
	request.Caller = Caller{Role: Editor}
	regeneration := harness.startAndProcess(t, request)
	stored, err := harness.module.GetRun(harness.ctx, regeneration.ID)
	if err != nil || stored.ResultVersion != 2 {
		t.Fatalf("regeneration=%#v err=%v", stored, err)
	}
	result, err := harness.module.GetCurrentResult(harness.ctx, H2HRecord, "m-1", PreToss, request.Inputs)
	if err != nil || result.Version != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestSQLModuleRetainsStaleResultWithoutReturningItAsCurrent(t *testing.T) {
	ctx, store := context.Background(), openWorkerTestStore(t)
	now := ParseTime("2026-09-08T10:00:00Z")
	config := DefaultConfiguration()
	module := NewSQLModule(config, store, ClockFunc(func() Time { return now }))
	run, err := module.StartRun(ctx, StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorkerWithOptions(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{TeamForm: {SampleSize: 5}}}, config, "test-worker", SQLWorkerOptions{Now: func() time.Time { return now }})
	if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	now = now.Add(16 * time.Minute)
	if _, err := module.GetCurrentResult(ctx, TeamForm, "m-1", PreToss, run.NormalizedInputs); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale result error = %v", err)
	}
	stored, err := module.GetRun(ctx, run.ID)
	if err != nil || stored.State != Succeeded || stored.ResultVersion != 1 {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestSQLModulePersistsCompletedCardSetStatus(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLRunStore(ctx, DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "card-set.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfiguration()
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewSQLModule(config, store, ClockFunc(func() Time { return now }))
	parent, err := module.StartRun(ctx, StartRequest{Target: CardTarget{CardSet: "pre_toss_v1"}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "players": []string{"p1"}, "venue": "wankhede", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	responses := map[TemplateID]GeneratedData{}
	for _, template := range config.CardSets["pre_toss_v1"] {
		responses[template] = GeneratedData{SampleSize: 10}
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: responses}, config, "test-worker")
	for range parent.Children {
		if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
			t.Fatalf("claimed=%v err=%v", claimed, err)
		}
	}
	freshParent, err := NewSQLModule(config, store, ClockFunc(func() Time { return now })).GetRun(ctx, parent.ID)
	if err != nil || freshParent.State != Succeeded || len(freshParent.Children) != 6 {
		t.Fatalf("parent=%#v err=%v", freshParent, err)
	}
}

func TestSQLModuleKeepsSuccessfulChildCurrentWhenCardSetChildFails(t *testing.T) {
	ctx, store := context.Background(), openWorkerTestStore(t)
	config := DefaultConfiguration()
	module := NewSQLModule(config, store, ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	parent, err := module.StartRun(ctx, StartRequest{Target: CardTarget{CardSet: "pre_toss_v1"}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "players": []string{"3"}, "venue": "4", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{"wins": 4}, SampleSize: 4}}}, config, "test-worker")
	for range parent.Children {
		if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
			t.Fatalf("claimed=%v err=%v", claimed, err)
		}
	}
	completed, err := module.GetRun(ctx, parent.ID)
	if err != nil || completed.State != CompletedWithErrors {
		t.Fatalf("parent=%#v err=%v", completed, err)
	}
	result, err := module.GetCurrentResult(ctx, H2HRecord, "m-1", PreToss, map[string]any{"team_a": "1", "team_b": "2", "venue": "4", "format": "t20", "latest_matches": 10})
	if err != nil || result.Version != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestSQLModuleRejectsUnapprovedMatchCount(t *testing.T) {
	module := NewSQLModule(DefaultConfiguration(), openWorkerTestStore(t), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	_, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: Last5Games}, Inputs: map[string]any{"players": []string{"p1"}, "format": "t20", "latest_matches": 99}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("free-form match count error = %v", err)
	}
}

func TestSQLModuleReconciliationJoinsActiveCardSetAndSkipsHealthyWrites(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLRunStore(ctx, DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "reconciliation.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfiguration()
	module := NewSQLModule(config, store, ClockFunc(func() Time { return ParseTime("2026-09-09T00:00:00Z") }))
	request := StartRequest{Target: CardTarget{CardSet: "pre_toss_v1"}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "players": []string{"3"}, "venue": "4", "format": "t20"}, Caller: Caller{Role: Scheduler}}
	first, err := module.StartRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := module.StartRun(ctx, request)
	if err != nil || joined.ID != first.ID {
		t.Fatalf("joined=%#v err=%v", joined, err)
	}
	responses := map[TemplateID]GeneratedData{}
	for _, template := range config.CardSets["pre_toss_v1"] {
		responses[template] = GeneratedData{SampleSize: 10}
	}
	worker := NewSQLWorker(store, &ScriptedCricketData{Responses: responses}, config, "test-worker")
	for range first.Children {
		if claimed, err := worker.ProcessOne(ctx); err != nil || !claimed {
			t.Fatalf("claimed=%v err=%v", claimed, err)
		}
	}
	healthy, err := module.StartRun(ctx, request)
	if err != nil || healthy.ID != "current-card-set" || len(healthy.Children) != 0 {
		t.Fatalf("healthy=%#v err=%v", healthy, err)
	}
}
