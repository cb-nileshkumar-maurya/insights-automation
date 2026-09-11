package tests

import (
	"context"
	"errors"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedTeamPhaseHistory struct {
	preferred map[int]map[string]PhaseMetric
	career    map[int]map[string]PhaseMetric
}

func (h fixedTeamPhaseHistory) PhaseProfiles(_ context.Context, _ []int, _ int, _ string, career bool) (map[int]map[string]PhaseMetric, error) {
	if career {
		return h.career, nil
	}
	return h.preferred, nil
}

func TestTeamPhaseProfilesReturnsBothTeamsAndFallsBackPerPhase(t *testing.T) {
	history := fixedTeamPhaseHistory{
		preferred: map[int]map[string]PhaseMetric{
			1: {"powerplay": {Innings: 10, Runs: 120, Deliveries: 60, Boundaries: 18, DotBalls: 20}, "middle": {Innings: 10, Runs: 140, Deliveries: 90, Wickets: 5, Boundaries: 16, DotBalls: 30}, "death": {Innings: 4, Runs: 80, Deliveries: 30, Wickets: 3, Boundaries: 12, DotBalls: 6}},
			2: {"powerplay": {Innings: 10, Runs: 110, Deliveries: 60, Boundaries: 15, DotBalls: 24}, "middle": {Innings: 10, Runs: 130, Deliveries: 90, Wickets: 7, Boundaries: 14, DotBalls: 35}, "death": {Innings: 10, Runs: 100, Deliveries: 60, Wickets: 8, Boundaries: 13, DotBalls: 20}},
		},
		career: map[int]map[string]PhaseMetric{1: {"death": {Innings: 20, Runs: 300, Deliveries: 120, Wickets: 12, Boundaries: 38, DotBalls: 40}}},
	}
	harness := newGenerationHarness(t, NewTeamPhaseProfilesGenerator(history))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: TeamPhaseProfiles}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	result, err := harness.module.GetCurrentResult(harness.ctx, TeamPhaseProfiles, "m-1", PreToss, run.NormalizedInputs)
	if err != nil {
		t.Fatal(err)
	}
	teams := result.Data["teams"].([]any)
	if len(teams) != 2 || result.Envelope.SampleSize != 70 || len(result.Envelope.Fallbacks) != 1 || result.Envelope.Fallbacks[0] != "team_1_death_career" {
		t.Fatalf("result=%#v", result)
	}
	death := teams[0].(map[string]any)["phases"].(map[string]any)["death"].(map[string]any)
	if !death["career_fallback"].(bool) || death["runs"].(float64) != 300 || death["rate"].(float64) != 15 {
		t.Fatalf("death=%#v", death)
	}
}

func TestTeamPhaseProfilesRejectsTestFormat(t *testing.T) {
	module := NewSQLModule(DefaultConfiguration(), openWorkerTestStore(t), ClockFunc(func() Time { return ParseTime("2026-09-09T00:00:00Z") }))
	_, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamPhaseProfiles}, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "test"}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("test format error = %v", err)
	}
}

func TestPhaseProfileFormatBoundaries(t *testing.T) {
	t20 := PhasesFor("t20")
	if len(t20) != 3 || t20[0].LastOver != 6 || t20[1].LastOver != 15 {
		t.Fatalf("T20 phases=%#v", t20)
	}
	odi := PhasesFor("odi")
	if len(odi) != 3 || odi[0].LastOver != 10 || odi[1].LastOver != 40 {
		t.Fatalf("ODI phases=%#v", odi)
	}
}
