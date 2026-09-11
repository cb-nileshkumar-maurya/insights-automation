package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
)

type staticEligibleMatches []EligibleMatch

func (matches staticEligibleMatches) EligibleMatches(context.Context) ([]EligibleMatch, error) {
	return matches, nil
}

func TestReconcilerRequestsTheNamedCardSetForEachEligibleMatch(t *testing.T) {
	module := NewSQLModule(DefaultConfiguration(), openWorkerTestStore(t), ClockFunc(func() Time { return ParseTime("2026-09-08T10:00:00Z") }))
	reconciler := NewReconciler(staticEligibleMatches{{MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"team_a": "india", "team_b": "australia", "players": []string{"p1"}, "venue": "wankhede", "format": "t20"}}}, module, "pre_toss_v1")
	runs, err := reconciler.Reconcile(context.Background())
	if err != nil || len(runs) != 1 || runs[0].Target.CardSet != "pre_toss_v1" || len(runs[0].Children) != 6 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
}
