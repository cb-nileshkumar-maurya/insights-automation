package generation

import (
	"context"
	"errors"
	"testing"
)

func TestNormalRequestRunsTemplateAndMakesCurrentResultAvailable(t *testing.T) {
	module := NewModule(DefaultConfiguration(), &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{
		H2HRecord: {Data: map[string]any{"wins": 3}, SampleSize: 3, SourceDataWindow: "2019-01-01..2026-01-01"},
	}}, NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))

	run, err := module.StartRun(context.Background(), StartRequest{
		Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss,
		Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"},
		Caller: Caller{Role: Scheduler},
	})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())

	got, err := module.GetCurrentResult(context.Background(), H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Envelope.SampleSize != 3 || got.Envelope.Template != H2HRecord {
		t.Fatalf("unexpected current result: %#v", got)
	}
}

func TestNormalRequestsJoinWorkAndRegenerationCreatesANewVersion(t *testing.T) {
	data := &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{"wins": 1}, SampleSize: 1}}}
	module := NewModule(DefaultConfiguration(), data, NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	request := StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"}, Caller: Caller{Role: Scheduler}}

	first, err := module.StartRun(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := module.StartRun(context.Background(), request)
	if err != nil || joined.ID != first.ID {
		t.Fatalf("normal request did not join work: %#v, %v", joined, err)
	}
	module.runAll(context.Background())
	request.Mode = Regeneration
	if _, err := module.StartRun(context.Background(), request); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("scheduler regeneration error = %v", err)
	}
	request.Caller.Role = Editor
	second, err := module.StartRun(context.Background(), request)
	if err != nil || second.ID == first.ID {
		t.Fatalf("editor regeneration = %#v, %v", second, err)
	}
	data.Responses[H2HRecord] = GeneratedData{Data: map[string]any{"wins": 2}, SampleSize: 2}
	module.runAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), H2HRecord, "m-1", PreToss, first.NormalizedInputs)
	if err != nil || result.Version != 2 {
		t.Fatalf("current version = %#v, %v", result, err)
	}
}

func TestFailedRegenerationKeepsPreviousCurrentResult(t *testing.T) {
	data := &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{}, SampleSize: 1}}}
	module := NewModule(DefaultConfiguration(), data, NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	request := StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"}, Caller: Caller{Role: Editor}}
	first, _ := module.StartRun(context.Background(), request)
	module.runAll(context.Background())
	request.Mode = Regeneration
	failure, _ := module.StartRun(context.Background(), request)
	data.Responses[H2HRecord] = GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: "replica unavailable"}}
	module.runAll(context.Background())
	run, _ := module.GetRun(context.Background(), failure.ID)
	current, err := module.GetCurrentResult(context.Background(), H2HRecord, "m-1", PreToss, first.NormalizedInputs)
	if run.State != Failed || err != nil || current.Version != 1 {
		t.Fatalf("run=%#v current=%#v err=%v", run, current, err)
	}
}

func TestTemplateRejectsFreeFormOptionsAndDefinesFormatPhases(t *testing.T) {
	config := DefaultConfiguration()
	if _, err := NewModule(config, &ScriptedCricketData{}, NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") })).StartRun(context.Background(), StartRequest{Target: CardTarget{Template: Last5Games}, Inputs: map[string]any{"players": []string{"p1"}, "format": "t20", "latest_matches": 99}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("free-form match count error = %v", err)
	}
	if len(PhasesFor("t20")) != 3 || PhasesFor("test") != nil {
		t.Fatalf("unexpected phases: %#v", PhasesFor("t20"))
	}
}

func TestCardSetKeepsSuccessfulChildCurrentWhenAnotherChildFails(t *testing.T) {
	data := &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{H2HRecord: {Data: map[string]any{"wins": 4}, SampleSize: 4}}}
	module := NewModule(DefaultConfiguration(), data, NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	parent, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{CardSet: "pre_toss_v1"}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "players": []string{"p1", "p2"}, "venue": "wankhede", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	completed, _ := module.GetRun(context.Background(), parent.ID)
	result, err := module.GetCurrentResult(context.Background(), H2HRecord, "m-1", PreToss, map[string]any{"team_a": "india", "team_b": "australia", "venue": "wankhede", "format": "t20", "latest_matches": 10})
	if completed.State != CompletedWithErrors || len(completed.Children) != 6 || err != nil || result.Version != 1 {
		t.Fatalf("parent=%#v result=%#v err=%v", completed, result, err)
	}
}

func TestStaleResultsAreRetainedButNotReturnedAsCurrent(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewModule(DefaultConfiguration(), &ScriptedCricketData{Responses: map[TemplateID]GeneratedData{TeamForm: {SampleSize: 5}}}, NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	now = now.Add(16 * 60 * 1e9)
	if _, err := module.GetCurrentResult(context.Background(), TeamForm, "m-1", PreToss, run.NormalizedInputs); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale result error = %v", err)
	}
	stored, _ := module.GetRun(context.Background(), run.ID)
	if stored.State != Succeeded || stored.ResultVersion != 1 {
		t.Fatalf("stale audit run = %#v", stored)
	}
}
