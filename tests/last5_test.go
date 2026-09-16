package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedLast5History struct{}

func (fixedLast5History) LastGames(context.Context, []int, int, int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error) {
	return map[int][]PlayerGame{}, map[int]PlayerCareerComparison{}, nil
}

func TestLast5GamesReturnsOneEntryPerPlayerFromABatchedPopulation(t *testing.T) {
	harness := newGenerationHarness(t, NewLast5GamesGenerator(fixedLast5History{}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: Last5Games}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"players": []string{"1", "2"}, "player_context": testPlayerContext(1, 2), "format": "t20"}})
	result, err := harness.module.GetCurrentResult(harness.ctx, Last5Games, "m-1", PreToss, run.NormalizedInputs)
	if err != nil {
		t.Fatal(err)
	}
	entries := result.Data["players"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestLast5GamesMarksShortAndMissingPlayerHistory(t *testing.T) {
	history := fixedLast5Games{games: map[int][]PlayerGame{1: {{MatchID: 10, BattingRuns: 42, BattingBalls: 31}}}, careers: map[int]PlayerCareerComparison{1: {BattingAverage: 30}}}
	harness := newGenerationHarness(t, NewLast5GamesGenerator(history))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: Last5Games}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"players": []string{"1", "2"}, "player_context": testPlayerContext(1, 2), "format": "t20", "latest_matches": 5}})
	result, err := harness.module.GetCurrentResult(harness.ctx, Last5Games, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.Fallbacks[0] != "player_available_history" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	entries := result.Data["players"].([]any)
	missing := entries[1].(map[string]any)
	if !missing["insufficient_sample"].(bool) || missing["available_history"].(float64) != 0 {
		t.Fatalf("missing=%#v", missing)
	}
}

type fixedLast5Games struct {
	games   map[int][]PlayerGame
	careers map[int]PlayerCareerComparison
}

func (history fixedLast5Games) LastGames(context.Context, []int, int, int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error) {
	return history.games, history.careers, nil
}
