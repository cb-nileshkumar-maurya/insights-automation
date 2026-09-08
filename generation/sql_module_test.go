package generation

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLModuleKeepsCurrentResultAfterModuleRestart(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLRunStore(ctx, DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
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
	request.Mode = Regeneration
	request.Caller = Caller{Role: Editor}
	failedRun, err := freshModule.StartRun(ctx, request)
	if err != nil {
		t.Fatal(err)
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
}

func TestSQLModulePersistsCompletedCardSetStatus(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLRunStore(ctx, DatabaseConfig{Dialect: SQLite, DSN: filepath.Join(t.TempDir(), "card-set.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
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
