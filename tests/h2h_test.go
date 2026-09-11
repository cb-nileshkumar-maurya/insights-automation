package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedH2HHistory []H2HMatchRecord

func (history fixedH2HHistory) FindH2H(context.Context, int, int, int, *int, int) ([]H2HMatchRecord, error) {
	return history, nil
}

func TestH2HRecordShowsAvailableHistoryWhenFewerMatchesExist(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	harness := newGenerationHarness(t, NewH2HGenerator(fixedH2HHistory{{MatchID: 1, Winner: 1, Margin: 10, WonByRuns: true, PlayedAt: now}, {MatchID: 2, Winner: 2, Margin: 3, PlayedAt: now.AddDate(0, 0, -1)}}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "latest_matches": 5}})
	result, err := harness.module.GetCurrentResult(harness.ctx, H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 2 || result.Envelope.Fallbacks[0] != "available_history" || result.Data["wins"] != float64(1) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestH2HRecordReportsInsufficientSampleWhenNoComparableMatchExists(t *testing.T) {
	harness := newGenerationHarness(t, NewH2HGenerator(fixedH2HHistory{}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20"}})
	failed, err := harness.module.GetRun(harness.ctx, run.ID)
	if err != nil || failed.State != Failed || failed.Failure.Kind != SampleFailure {
		t.Fatalf("run=%#v err=%v", failed, err)
	}
}

func TestH2HRecordRejectsRemovedHomeContextFilter(t *testing.T) {
	if _, err := NewSQLModule(DefaultConfiguration(), openWorkerTestStore(t), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") })).StartRun(context.Background(), StartRequest{Target: CardTarget{Template: H2HRecord}, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "context": "home"}}); err == nil {
		t.Fatal("home context should not be accepted in V1")
	}
}
