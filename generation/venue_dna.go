package generation

import (
	"context"
	"database/sql"
	"fmt"
)

const venueMinimumSample = 10

type VenueMatch struct {
	FirstInningsScore int
	FirstInningsTeam  int
	Winner            int
}
type VenueHistory interface {
	VenueMatches(context.Context, int, int, string) ([]VenueMatch, error)
	CountryMatches(context.Context, int, int, string) ([]VenueMatch, error)
}
type VenuePhaseHistory interface {
	VenuePhaseBenchmarks(context.Context, int, int, string, bool) (map[string]any, error)
}
type VenueDNAGenerator struct{ history VenueHistory }

func NewVenueDNAGenerator(history VenueHistory) *VenueDNAGenerator {
	return &VenueDNAGenerator{history: history}
}
func (g *VenueDNAGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != VenueDNA {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "Venue DNA generator received another template"}}
	}
	venue, err := inputID(query.Inputs, "venue")
	if err != nil {
		return invalidH2H(err)
	}
	format, err := formatID(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	window, _ := query.Inputs["window"].(string)
	matches, err := g.history.VenueMatches(ctx, venue, format, window)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	fallbacks := []string{}
	if len(matches) < venueMinimumSample {
		matches, err = g.history.CountryMatches(ctx, venue, format, window)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
		fallbacks = append(fallbacks, "country_level")
	}
	if len(matches) < venueMinimumSample {
		return GeneratedData{Err: &RunFailure{Kind: SampleFailure, Message: "fewer than ten comparable venue or country matches"}}
	}
	firstTotal, batFirstWins, chaseWins, high, low := 0, 0, 0, matches[0].FirstInningsScore, matches[0].FirstInningsScore
	for _, match := range matches {
		firstTotal += match.FirstInningsScore
		if match.FirstInningsScore > high {
			high = match.FirstInningsScore
		}
		if match.FirstInningsScore < low {
			low = match.FirstInningsScore
		}
		if match.Winner == match.FirstInningsTeam {
			batFirstWins++
		} else if match.Winner != 0 {
			chaseWins++
		}
	}
	phaseBenchmarks := map[string]any{}
	if phases, ok := g.history.(VenuePhaseHistory); ok {
		phaseBenchmarks, err = phases.VenuePhaseBenchmarks(ctx, venue, format, window, len(fallbacks) > 0)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
	}
	data := map[string]any{"average_first_innings_score": float64(firstTotal) / float64(len(matches)), "bat_first_win_rate": float64(batFirstWins) * 100 / float64(len(matches)), "chase_win_rate": float64(chaseWins) * 100 / float64(len(matches)), "high_total": high, "low_total": low, "match_phase_benchmarks": phaseBenchmarks}
	return GeneratedData{Data: data, SampleSize: len(matches), Fallbacks: fallbacks, SourceDataWindow: fmt.Sprintf("%s window", window)}
}

type MariaDBVenueHistory struct{ db *sql.DB }

func NewMariaDBVenueHistory(db *sql.DB) *MariaDBVenueHistory { return &MariaDBVenueHistory{db: db} }
func OpenMariaDBVenueDNAGenerator(ctx context.Context) (*VenueDNAGenerator, *sql.DB, error) {
	db, err := OpenReadReplica(ctx)
	if err != nil {
		return nil, nil, err
	}
	return NewVenueDNAGenerator(NewMariaDBVenueHistory(db)), db, nil
}
func (h *MariaDBVenueHistory) VenueMatches(ctx context.Context, venue, format int, window string) ([]VenueMatch, error) {
	return h.find(ctx, `m.venueid = ?`, []any{venue}, format, window)
}
func (h *MariaDBVenueHistory) CountryMatches(ctx context.Context, venue, format int, window string) ([]VenueMatch, error) {
	return h.find(ctx, `m.venueid IN (SELECT id FROM krik_match_venue WHERE country_id = (SELECT country_id FROM krik_match_venue WHERE id = ?))`, []any{venue}, format, window)
}
func (h *MariaDBVenueHistory) find(ctx context.Context, population string, args []any, format int, window string) ([]VenueMatch, error) {
	dateClause := ""
	if window == "since_2024" {
		dateClause = " AND m.startdt >= '2024-01-01'"
	}
	args = append(args, format)
	rows, err := h.db.QueryContext(ctx, `SELECT i.runs, i.battingTeamId, COALESCE(m.winner, 0) FROM krik_match_archive m JOIN stats_import3_dump_innings_tbl i ON i.matchId = m.id AND i.inningsId = 1 WHERE `+population+` AND m.match_type_id = ? AND m.isArchived = 1`+dateClause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := []VenueMatch{}
	for rows.Next() {
		var match VenueMatch
		if err := rows.Scan(&match.FirstInningsScore, &match.FirstInningsTeam, &match.Winner); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func (h *MariaDBVenueHistory) VenuePhaseBenchmarks(ctx context.Context, venue, format int, window string, countryFallback bool) (map[string]any, error) {
	population, args := "m.venueid = ?", []any{venue}
	if countryFallback {
		population = "m.venueid IN (SELECT id FROM krik_match_venue WHERE country_id = (SELECT country_id FROM krik_match_venue WHERE id = ?))"
	}
	dateClause := ""
	if window == "since_2024" {
		dateClause = " AND m.startdt >= '2024-01-01'"
	}
	args = append(args, format)
	rows, err := h.db.QueryContext(ctx, `WITH comparable_matches AS (SELECT m.id FROM krik_match_archive m WHERE `+population+` AND m.match_type_id = ? AND m.isArchived = 1`+dateClause+`) SELECT CASE WHEN b.ballNbr <= 6 THEN 'powerplay' WHEN b.ballNbr <= 15 THEN 'middle' ELSE 'death' END, AVG(b.totalRuns) FROM krik_ball_by_ball_archive b JOIN comparable_matches c ON c.id = b.matchId GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	benchmarks := map[string]any{}
	for rows.Next() {
		var phase string
		var average float64
		if err := rows.Scan(&phase, &average); err != nil {
			return nil, err
		}
		benchmarks[phase] = map[string]any{"average_runs_per_delivery": average}
	}
	return benchmarks, rows.Err()
}
