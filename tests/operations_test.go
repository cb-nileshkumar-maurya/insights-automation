package tests

import (
	"testing"
	"time"

	"github.com/cricbuzz/insights-automation/generation"
)

func TestAnalyticsReadModelDecisionRequiresASustainedBreach(t *testing.T) {
	now := generation.ParseTime("2026-09-09T00:15:00Z")
	measurements := []generation.BatchMeasurement{
		{MeasuredAt: now.Add(-15 * time.Minute), DatabaseP95: 31 * time.Second},
		{MeasuredAt: now, DatabaseP95: 31 * time.Second},
	}
	decision := generation.EvaluateAnalyticsReadModelThresholds(measurements)
	if !decision.AdoptAnalyticsReadModel || decision.Reasons[0] != "database_p95_over_30_seconds_for_15_minutes" {
		t.Fatalf("decision=%#v", decision)
	}
	measurements[1].DatabaseP95 = 29 * time.Second
	if decision := generation.EvaluateAnalyticsReadModelThresholds(measurements); decision.AdoptAnalyticsReadModel {
		t.Fatalf("short breach must not trigger: %#v", decision)
	}
}
