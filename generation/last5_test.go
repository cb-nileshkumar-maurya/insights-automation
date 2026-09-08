package generation

import (
	"context"
	"testing"
)

type fixedLast5History struct{}

func (fixedLast5History) LastGames(context.Context, []int, int, int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error) {
	return map[int][]PlayerGame{}, map[int]PlayerCareerComparison{}, nil
}

func TestLast5GamesReturnsOneEntryPerPlayerFromABatchedPopulation(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewModule(DefaultConfiguration(), NewLast5GamesGenerator(fixedLast5History{}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: Last5Games}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"players": []string{"1", "2"}, "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), Last5Games, "m-1", PreToss, run.NormalizedInputs)
	if err != nil {
		t.Fatal(err)
	}
	entries := result.Data["players"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
}
