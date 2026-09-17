package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type fixedH2HHistory []H2HMatchRecord

func (history fixedH2HHistory) FindH2H(context.Context, int, int, int, int) ([]H2HMatchRecord, error) {
	return history, nil
}

func TestH2HRecordShowsAvailableHistoryWhenFewerMatchesExist(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	history := fixedH2HHistory{
		{MatchID: 3, Team1ID: 1, Team1Name: "Alpha", Team2ID: 2, Team2Name: "Beta", Winner: 1, ResultType: "win", Margin: 1, WonByRuns: true, PlayedAt: now},
		{MatchID: 2, Team1ID: 2, Team1Name: "Beta", Team2ID: 1, Team2Name: "Alpha", ResultType: "draw", PlayedAt: now.AddDate(0, 0, -1)},
		{MatchID: 1, Team1ID: 1, Team1Name: "Alpha", Team2ID: 2, Team2Name: "Beta", ResultType: "abandoned", PlayedAt: now.AddDate(0, 0, -2)},
	}
	harness := newGenerationHarness(t, NewH2HGenerator(history))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: H2HRecord}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "latest_matches": 15}})
	result, err := harness.module.GetCurrentResult(harness.ctx, H2HRecord, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 3 || result.Envelope.Fallbacks[0] != "available_history" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	summary := result.Data["summary"].(map[string]any)
	if summary["matches_played"].(float64) != 3 || summary["draws"].(float64) != 1 || summary["no_results"].(float64) != 1 {
		t.Fatalf("summary=%#v", summary)
	}
	teams := summary["teams"].([]any)
	alpha := teams[0].(map[string]any)
	beta := teams[1].(map[string]any)
	if alpha["team_id"].(float64) != 1 || alpha["team_name"] != "Alpha" || alpha["wins"].(float64) != 1 || alpha["losses"].(float64) != 0 || beta["team_id"].(float64) != 2 || beta["wins"].(float64) != 0 || beta["losses"].(float64) != 1 {
		t.Fatalf("teams=%#v", teams)
	}
	records := result.Data["head_to_head"].([]any)
	first := records[0].(map[string]any)
	if len(records) != 3 || first["start_date"] != "2026-09-08" || first["team1_id"].(float64) != 1 || first["team2_id"].(float64) != 2 || first["winner_team_id"].(float64) != 1 || first["outcome"] != "win" || first["result_string"] != "Alpha won by 1 run" {
		t.Fatalf("records=%#v", records)
	}
	if records[1].(map[string]any)["outcome"] != "draw" || records[2].(map[string]any)["outcome"] != "no_result" {
		t.Fatalf("records=%#v", records)
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

func TestH2HRecordRejectsVenueFilter(t *testing.T) {
	if _, err := NewSQLModule(DefaultConfiguration(), openWorkerTestStore(t), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") })).StartRun(context.Background(), StartRequest{Target: CardTarget{Template: H2HRecord}, Inputs: map[string]any{"team_a": "1", "team_b": "2", "format": "t20", "venue": "4"}}); err == nil {
		t.Fatal("venue filter should not be accepted")
	}
}
