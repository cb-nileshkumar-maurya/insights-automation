package generation

import (
	"context"
	"database/sql"
	"fmt"
)

const phaseMinimumInnings = 10

type PhaseMetric struct{ Innings, Runs, Wickets, Deliveries, Boundaries, DotBalls int }
type TeamPhaseHistory interface {
	PhaseProfiles(context.Context, []int, int, string, bool) (map[int]map[string]PhaseMetric, error)
}
type TeamPhaseProfilesGenerator struct{ history TeamPhaseHistory }

func NewTeamPhaseProfilesGenerator(history TeamPhaseHistory) *TeamPhaseProfilesGenerator {
	return &TeamPhaseProfilesGenerator{history: history}
}
func (g *TeamPhaseProfilesGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != TeamPhaseProfiles {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "Team Phase Profiles generator received another template"}}
	}
	a, err := inputID(query.Inputs, "team_a")
	if err != nil {
		return invalidH2H(err)
	}
	b, err := inputID(query.Inputs, "team_b")
	if err != nil {
		return invalidH2H(err)
	}
	format, err := formatID(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	window := query.Inputs["window"].(string)
	preferred, err := g.history.PhaseProfiles(ctx, []int{a, b}, format, window, false)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	career, err := g.history.PhaseProfiles(ctx, []int{a, b}, format, "career", true)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	entries, fallbacks, sample := make([]any, 0, 2), []string{}, 0
	for _, team := range []int{a, b} {
		values := map[string]any{}
		for _, phase := range PhasesFor(query.Inputs["format"].(string)) {
			metric := preferred[team][phase]
			fallback := false
			if metric.Innings < phaseMinimumInnings {
				metric, fallback = career[team][phase], true
				fallbacks = append(fallbacks, fmt.Sprintf("team_%d_%s_career", team, phase))
			}
			sample += metric.Innings
			values[phase] = phaseOutput(metric, fallback)
		}
		entries = append(entries, map[string]any{"team": team, "phases": values})
	}
	return GeneratedData{Data: map[string]any{"teams": entries}, SampleSize: sample, Fallbacks: fallbacks, SourceDataWindow: window}
}
func phaseOutput(metric PhaseMetric, fallback bool) map[string]any {
	rate, boundary, dot := 0.0, 0.0, 0.0
	if metric.Deliveries > 0 {
		rate = float64(metric.Runs) * 6 / float64(metric.Deliveries)
		boundary = float64(metric.Boundaries) * 100 / float64(metric.Deliveries)
		dot = float64(metric.DotBalls) * 100 / float64(metric.Deliveries)
	}
	return map[string]any{"innings": metric.Innings, "runs": metric.Runs, "wickets": metric.Wickets, "rate": rate, "boundary_percentage": boundary, "dot_percentage": dot, "career_fallback": fallback}
}

// MariaDBTeamPhaseHistory reads deliveries only after first defining the
// comparable match population. That prevents a team-level aggregate from
// silently including matches outside the requested format and window.
type MariaDBTeamPhaseHistory struct{ db *sql.DB }

func NewMariaDBTeamPhaseHistory(db *sql.DB) *MariaDBTeamPhaseHistory {
	return &MariaDBTeamPhaseHistory{db: db}
}

func (h *MariaDBTeamPhaseHistory) PhaseProfiles(ctx context.Context, teams []int, format int, window string, career bool) (map[int]map[string]PhaseMetric, error) {
	if len(teams) == 0 {
		return map[int]map[string]PhaseMetric{}, nil
	}
	marks, teamArgs := playerPlaceholders(teams)
	populationArgs := append([]any{}, teamArgs...)
	populationArgs = append(populationArgs, teamArgs...)
	populationArgs = append(populationArgs, format)
	dateClause := ""
	if !career && window == "since_2024" {
		dateClause = " AND m.startdt >= '2024-01-01'"
	}

	phase := phaseNameExpression(format)
	statement := teamPhaseProfileStatement(marks, phase, dateClause)
	args := append(populationArgs, teamArgs...)
	rows, err := h.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := map[int]map[string]PhaseMetric{}
	for rows.Next() {
		var team int
		var phaseName string
		var metric PhaseMetric
		if err := rows.Scan(&team, &phaseName, &metric.Innings, &metric.Runs, &metric.Wickets, &metric.Deliveries, &metric.Boundaries, &metric.DotBalls); err != nil {
			return nil, err
		}
		if profiles[team] == nil {
			profiles[team] = map[string]PhaseMetric{}
		}
		profiles[team][phaseName] = metric
	}
	return profiles, rows.Err()
}

func phaseNameExpression(format int) string {
	if format == 2 { // ODI
		return "CASE WHEN FLOOR((b.ballNbr - 1) / 6) + 1 <= 10 THEN 'powerplay' WHEN FLOOR((b.ballNbr - 1) / 6) + 1 <= 40 THEN 'middle' ELSE 'death' END"
	}
	return "CASE WHEN FLOOR((b.ballNbr - 1) / 6) + 1 <= 6 THEN 'powerplay' WHEN FLOOR((b.ballNbr - 1) / 6) + 1 <= 15 THEN 'middle' ELSE 'death' END"
}

func teamPhaseProfileStatement(marks, phase, dateClause string) string {
	return `WITH comparable_matches AS (
		SELECT m.id FROM krik_match_archive m
		WHERE (m.teama IN (` + marks + `) OR m.teamb IN (` + marks + `))
		  AND m.match_type_id = ? AND m.isArchived = 1` + dateClause + `
	) SELECT b.battingTeamId, ` + phase + `,
		COUNT(DISTINCT CONCAT(b.matchId, ':', b.inningsId)),
		COALESCE(SUM(b.totalRuns), 0),
		COALESCE(SUM(CASE WHEN NULLIF(TRIM(b.wicketCode), '') IS NOT NULL THEN 1 ELSE 0 END), 0),
		COUNT(*),
		COALESCE(SUM(CASE WHEN b.legalRuns IN (4, 6) THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN b.totalRuns = 0 THEN 1 ELSE 0 END), 0)
		FROM krik_ball_by_ball_archive b
		JOIN comparable_matches c ON c.id = b.matchId
		WHERE b.battingTeamId IN (` + marks + `)
		GROUP BY b.battingTeamId, ` + phase
}
