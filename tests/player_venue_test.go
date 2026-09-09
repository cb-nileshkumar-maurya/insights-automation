package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedPlayerVenueHistory struct{ batting, bowling, careerBatting, careerBowling map[int]PlayerVenueDiscipline }

func (history fixedPlayerVenueHistory) PlayerVenueStats(_ context.Context, _ []int, _ int, _ int, _ string, career bool) (map[int]PlayerVenueDiscipline, map[int]PlayerVenueDiscipline, error) {
	if career {
		return history.careerBatting, history.careerBowling, nil
	}
	return history.batting, history.bowling, nil
}

func TestPlayerVenueStatsFallsBackPerDiscipline(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	history := fixedPlayerVenueHistory{batting: map[int]PlayerVenueDiscipline{1: {Innings: 15, Runs: 400}, 2: {Innings: 2, Runs: 20}}, bowling: map[int]PlayerVenueDiscipline{1: {Innings: 2, Wickets: 1}, 2: {Innings: 15, Wickets: 20}}, careerBatting: map[int]PlayerVenueDiscipline{2: {Innings: 30, Runs: 900}}, careerBowling: map[int]PlayerVenueDiscipline{1: {Innings: 25, Wickets: 30}}}
	module := NewModule(DefaultConfiguration(), NewPlayerVenueGenerator(history), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: PlayerStatsAtVenue}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"players": []string{"1", "2"}, "venue": "10", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.ProcessAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), PlayerStatsAtVenue, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || len(result.Envelope.Fallbacks) != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.Envelope.Fallbacks[0] != "player_1_bowling_career" || result.Envelope.Fallbacks[1] != "player_2_batting_career" {
		t.Fatalf("fallbacks=%#v", result.Envelope.Fallbacks)
	}
}
