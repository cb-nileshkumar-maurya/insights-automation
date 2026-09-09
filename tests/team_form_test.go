package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedTeamFormHistory map[int][]TeamFormMatch

func (history fixedTeamFormHistory) FindTeamForm(_ context.Context, team, _ int, _ int) ([]TeamFormMatch, error) {
	return history[team], nil
}

func TestTeamFormShowsAvailableHistoryForEachTeam(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewModule(DefaultConfiguration(), NewTeamFormGenerator(fixedTeamFormHistory{1: {{MatchID: 1, Winner: 1, Margin: 10, WonByRuns: true, PlayedAt: now}}, 2: {{MatchID: 2, Winner: 1, Margin: 3, PlayedAt: now}}}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "latest_matches": 5}})
	if err != nil {
		t.Fatal(err)
	}
	module.ProcessAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), TeamForm, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 2 || result.Envelope.Fallbacks[0] != "available_history" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestTeamFormReportsInsufficientSampleWhenNeitherTeamHasHistory(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewModule(DefaultConfiguration(), NewTeamFormGenerator(fixedTeamFormHistory{}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.ProcessAll(context.Background())
	failed, _ := module.GetRun(context.Background(), run.ID)
	if failed.State != Failed || failed.Failure.Kind != SampleFailure {
		t.Fatalf("run=%#v", failed)
	}
}
