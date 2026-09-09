package generation

import (
	"context"
	"errors"
	"strings"
	"testing"
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
	module := NewModule(DefaultConfiguration(), NewTeamPhaseProfilesGenerator(history), NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-09T00:00:00Z") }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamPhaseProfiles}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), TeamPhaseProfiles, "m-1", PreToss, run.NormalizedInputs)
	if err != nil {
		t.Fatal(err)
	}
	teams := result.Data["teams"].([]any)
	if len(teams) != 2 || result.Envelope.SampleSize != 70 || len(result.Envelope.Fallbacks) != 1 || result.Envelope.Fallbacks[0] != "team_1_death_career" {
		t.Fatalf("result=%#v", result)
	}
	death := teams[0].(map[string]any)["phases"].(map[string]any)["death"].(map[string]any)
	if !death["career_fallback"].(bool) || death["runs"].(int) != 300 || death["rate"].(float64) != 15 {
		t.Fatalf("death=%#v", death)
	}
}

func TestTeamPhaseProfilesRejectsTestFormat(t *testing.T) {
	module := NewModule(DefaultConfiguration(), NewTeamPhaseProfilesGenerator(fixedTeamPhaseHistory{}), NewMemoryRunStore(), ClockFunc(func() Time { return ParseTime("2026-09-09T00:00:00Z") }))
	_, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: TeamPhaseProfiles}, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "test"}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("test format error = %v", err)
	}
}

func TestPhaseProfileQueryUsesConstrainedMatchPopulation(t *testing.T) {
	statement := teamPhaseProfileStatement("?,?", "CASE WHEN b.ballNbr <= 6 THEN 'powerplay' END", "")
	if !containsAll(statement, "WITH comparable_matches", "JOIN comparable_matches", "b.battingTeamId IN (?,?)") {
		t.Fatal("delivery query must join the selected match population")
	}
}

func TestPhaseProfileFormatBoundaries(t *testing.T) {
	if !containsAll(phaseNameExpression(3), "<= 6", "<= 15") {
		t.Fatalf("T20 phases=%s", phaseNameExpression(3))
	}
	if !containsAll(phaseNameExpression(2), "<= 10", "<= 40") {
		t.Fatalf("ODI phases=%s", phaseNameExpression(2))
	}
}

func containsAll(value string, expected ...string) bool {
	for _, part := range expected {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
