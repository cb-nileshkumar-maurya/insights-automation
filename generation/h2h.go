package generation

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

type H2HMatchRecord struct {
	MatchID            int
	Winner             int
	Margin             int
	WonByRuns          bool
	PlayedAt           time.Time
	FirstInningsScore  *int
	SecondInningsScore *int
}
type H2HHistory interface {
	FindH2H(context.Context, int, int, int, *int, int) ([]H2HMatchRecord, error)
}
type H2HGenerator struct{ history H2HHistory }

func NewH2HGenerator(history H2HHistory) *H2HGenerator { return &H2HGenerator{history: history} }

func (g *H2HGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != H2HRecord {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "H2H generator received another template"}}
	}
	teamA, err := inputID(query.Inputs, "team_a")
	if err != nil {
		return invalidH2H(err)
	}
	teamB, err := inputID(query.Inputs, "team_b")
	if err != nil {
		return invalidH2H(err)
	}
	format, err := formatID(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	latest := query.Inputs["latest_matches"].(int)
	var venue *int
	if raw, ok := query.Inputs["venue"]; ok {
		value, err := inputID(query.Inputs, "venue")
		if err != nil {
			return invalidH2H(err)
		}
		venue = &value
		_ = raw
	}
	matches, err := g.history.FindH2H(ctx, teamA, teamB, format, venue, latest)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	if len(matches) == 0 {
		return GeneratedData{Err: &RunFailure{Kind: SampleFailure, Message: "no comparable H2H matches"}}
	}
	wins, losses, draws, firstTotal, firstCount, secondTotal, secondCount := 0, 0, 0, 0, 0, 0, 0
	margins, recent := make([]map[string]any, 0, len(matches)), make([]string, 0, len(matches))
	for _, match := range matches {
		outcome := "draw"
		if match.Winner == teamA {
			wins++
			outcome = "win"
		} else if match.Winner == teamB {
			losses++
			outcome = "loss"
		} else {
			draws++
		}
		recent = append(recent, outcome)
		marginType := "wickets"
		if match.WonByRuns {
			marginType = "runs"
		}
		margins = append(margins, map[string]any{"match_id": match.MatchID, "outcome": outcome, "value": match.Margin, "type": marginType, "played_at": match.PlayedAt.Format("2006-01-02")})
		if match.FirstInningsScore != nil {
			firstTotal += *match.FirstInningsScore
			firstCount++
		}
		if match.SecondInningsScore != nil {
			secondTotal += *match.SecondInningsScore
			secondCount++
		}
	}
	data := map[string]any{"team_a": teamA, "team_b": teamB, "wins": wins, "losses": losses, "draws": draws, "win_rate": float64(wins) * 100 / float64(len(matches)), "margins": margins, "recent_sequence": recent, "average_first_innings_score": average(firstTotal, firstCount), "average_second_innings_score": average(secondTotal, secondCount)}
	fallbacks := []string{}
	if len(matches) < latest {
		fallbacks = append(fallbacks, "available_history")
	}
	return GeneratedData{Data: data, SampleSize: len(matches), Fallbacks: fallbacks, SourceDataWindow: matches[len(matches)-1].PlayedAt.Format("2006-01-02") + ".." + matches[0].PlayedAt.Format("2006-01-02")}
}
func invalidH2H(err error) GeneratedData {
	return GeneratedData{Err: &RunFailure{Kind: ValidationFailure, Message: err.Error()}}
}
func average(total, count int) any {
	if count == 0 {
		return nil
	}
	return float64(total) / float64(count)
}
func inputID(inputs map[string]any, name string) (int, error) {
	raw, ok := inputs[name]
	if !ok {
		return 0, fmt.Errorf("%s is required", name)
	}
	value, err := strconv.Atoi(fmt.Sprint(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive stable ID", name)
	}
	return value, nil
}
func formatID(inputs map[string]any) (int, error) {
	switch inputs["format"] {
	case "t20":
		return 3, nil
	case "odi":
		return 2, nil
	default:
		return 0, fmt.Errorf("format is not supported for H2H")
	}
}

type MariaDBH2HHistory struct{ db *sql.DB }

func NewMariaDBH2HHistory(db *sql.DB) *MariaDBH2HHistory { return &MariaDBH2HHistory{db: db} }
func OpenMariaDBH2HGenerator(ctx context.Context) (*H2HGenerator, *sql.DB, error) {
	db, err := OpenReadReplica(ctx)
	if err != nil {
		return nil, nil, err
	}
	return NewH2HGenerator(NewMariaDBH2HHistory(db)), db, nil
}
func (h *MariaDBH2HHistory) FindH2H(ctx context.Context, teamA, teamB, format int, venue *int, latest int) ([]H2HMatchRecord, error) {
	args := []any{teamA, teamB, teamB, teamA, format}
	venueClause := ""
	if venue != nil {
		venueClause = " AND m.venueid = ?"
		args = append(args, *venue)
	}
	args = append(args, latest)
	rows, err := h.db.QueryContext(ctx, `SELECT m.id, COALESCE(m.winner, 0), COALESCE(r.winningMargin, 0), COALESCE(r.winByRuns, 0), m.startdt, MAX(CASE WHEN i.inningsId = 1 THEN i.runs END), MAX(CASE WHEN i.inningsId = 2 THEN i.runs END) FROM krik_match_archive m LEFT JOIN stats_import3_dump_matchresult_tbl r ON r.matchId = m.id LEFT JOIN stats_import3_dump_innings_tbl i ON i.matchId = m.id WHERE ((m.teama = ? AND m.teamb = ?) OR (m.teama = ? AND m.teamb = ?)) AND m.match_type_id = ? AND m.isArchived = 1`+venueClause+` GROUP BY m.id, m.winner, r.winningMargin, r.winByRuns, m.startdt ORDER BY m.startdt DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []H2HMatchRecord{}
	for rows.Next() {
		var result H2HMatchRecord
		var first, second sql.NullInt64
		if err := rows.Scan(&result.MatchID, &result.Winner, &result.Margin, &result.WonByRuns, &result.PlayedAt, &first, &second); err != nil {
			return nil, err
		}
		if first.Valid {
			value := int(first.Int64)
			result.FirstInningsScore = &value
		}
		if second.Valid {
			value := int(second.Int64)
			result.SecondInningsScore = &value
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
