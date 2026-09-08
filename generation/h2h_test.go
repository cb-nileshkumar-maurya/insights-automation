package generation

import (
	"context"
	"testing"
)

type fixedH2HHistory []H2HMatchRecord

func (history fixedH2HHistory) FindH2H(context.Context, int, int, int, *int, int) ([]H2HMatchRecord, error) {
	return history, nil
}

func TestH2HRecordShowsAvailableHistoryWhenFewerMatchesExist(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	module := NewModule(DefaultConfiguration(), NewH2HGenerator(fixedH2HHistory{{MatchID: 1, Winner: 1, Margin: 10, WonByRuns: true, PlayedAt: now}, {MatchID: 2, Winner: 2, Margin: 3, PlayedAt: now.AddDate(0, 0, -1)}}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "latest_matches": 5}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 2 || result.Envelope.Fallbacks[0] != "available_history" || result.Data["wins"] != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
