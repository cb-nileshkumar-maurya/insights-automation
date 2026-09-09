package generation

import "time"

const (
	analyticsDatabaseP95Limit = 30 * time.Second
	analyticsEndToEndP95Limit = 90 * time.Second
	analyticsBreachDuration   = 15 * time.Minute
)

// BatchMeasurement is the operational input emitted once per six-template
// batch. p95 values are measured by the hosting metrics backend, while the
// replica owner supplies the separately agreed read-load signal.
type BatchMeasurement struct {
	MeasuredAt            time.Time
	DatabaseP95           time.Duration
	QueueWaitPlusExecute  time.Duration
	ReplicaReadLoadBreach bool
}

type AnalyticsReadModelDecision struct {
	AdoptAnalyticsReadModel bool
	Reasons                 []string
}

// EvaluateAnalyticsReadModelThresholds recommends an analytics read model
// only after the same threshold has been breached for fifteen minutes.
func EvaluateAnalyticsReadModelThresholds(measurements []BatchMeasurement) AnalyticsReadModelDecision {
	if len(measurements) == 0 {
		return AnalyticsReadModelDecision{}
	}
	latest := measurements[len(measurements)-1].MeasuredAt
	cutoff := latest.Add(-analyticsBreachDuration)
	database, endToEnd, replica := true, true, true
	seen := false
	for _, measurement := range measurements {
		if measurement.MeasuredAt.Before(cutoff) {
			continue
		}
		seen = true
		database = database && measurement.DatabaseP95 > analyticsDatabaseP95Limit
		endToEnd = endToEnd && measurement.QueueWaitPlusExecute > analyticsEndToEndP95Limit
		replica = replica && measurement.ReplicaReadLoadBreach
	}
	if !seen {
		return AnalyticsReadModelDecision{}
	}
	decision := AnalyticsReadModelDecision{}
	if database {
		decision.Reasons = append(decision.Reasons, "database_p95_over_30_seconds_for_15_minutes")
	}
	if endToEnd {
		decision.Reasons = append(decision.Reasons, "queue_wait_plus_execution_p95_over_90_seconds_for_15_minutes")
	}
	if replica {
		decision.Reasons = append(decision.Reasons, "replica_read_load_breach_for_15_minutes")
	}
	decision.AdoptAnalyticsReadModel = len(decision.Reasons) > 0
	return decision
}
