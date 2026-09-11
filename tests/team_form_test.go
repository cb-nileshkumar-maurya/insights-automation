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
	harness := newGenerationHarness(t, NewTeamFormGenerator(fixedTeamFormHistory{1: {{MatchID: 1, Winner: 1, Margin: 10, WonByRuns: true, PlayedAt: now}}, 2: {{MatchID: 2, Winner: 1, Margin: 3, PlayedAt: now}}}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "latest_matches": 5}})
	result, err := harness.module.GetCurrentResult(harness.ctx, TeamForm, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 2 || result.Envelope.Fallbacks[0] != "available_history" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestTeamFormReportsInsufficientSampleWhenNeitherTeamHasHistory(t *testing.T) {
	harness := newGenerationHarness(t, NewTeamFormGenerator(fixedTeamFormHistory{}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: TeamForm}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	failed, err := harness.module.GetRun(harness.ctx, run.ID)
	if err != nil || failed.State != Failed || failed.Failure.Kind != SampleFailure {
		t.Fatalf("run=%#v err=%v", failed, err)
	}
}
