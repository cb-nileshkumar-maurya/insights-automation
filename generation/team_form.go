package generation

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type TeamFormMatch struct {
	MatchID   int
	Winner    int
	Margin    int
	WonByRuns bool
	PlayedAt  time.Time
}
type TeamFormHistory interface {
	FindTeamForm(context.Context, int, int, int) ([]TeamFormMatch, error)
}
type TeamFormGenerator struct{ history TeamFormHistory }

func NewTeamFormGenerator(history TeamFormHistory) *TeamFormGenerator {
	return &TeamFormGenerator{history: history}
}
func (g *TeamFormGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != TeamForm {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "Team Form generator received another template"}}
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
	a, err := g.history.FindTeamForm(ctx, teamA, format, latest)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	b, err := g.history.FindTeamForm(ctx, teamB, format, latest)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	if len(a) == 0 && len(b) == 0 {
		return GeneratedData{Err: &RunFailure{Kind: SampleFailure, Message: "no comparable team form matches"}}
	}
	data := map[string]any{"teams": []any{teamFormEntry(teamA, a, latest), teamFormEntry(teamB, b, latest)}}
	fallbacks := []string{}
	if len(a) < latest || len(b) < latest {
		fallbacks = append(fallbacks, "available_history")
	}
	return GeneratedData{Data: data, SampleSize: len(a) + len(b), Fallbacks: fallbacks, SourceDataWindow: "latest " + fmt.Sprint(latest) + " matches"}
}
func teamFormEntry(team int, matches []TeamFormMatch, requested int) map[string]any {
	wins, losses, draws := 0, 0, 0
	recent := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		outcome := "draw"
		if match.Winner == team {
			wins++
			outcome = "win"
		} else if match.Winner != 0 {
			losses++
			outcome = "loss"
		} else {
			draws++
		}
		kind := "wickets"
		if match.WonByRuns {
			kind = "runs"
		}
		recent = append(recent, map[string]any{"match_id": match.MatchID, "outcome": outcome, "margin": match.Margin, "margin_type": kind, "played_at": match.PlayedAt.Format("2006-01-02")})
	}
	return map[string]any{"team": team, "matches": recent, "wins": wins, "losses": losses, "draws": draws, "available_history": len(matches), "requested_history": requested, "insufficient_sample": len(matches) == 0}
}

type MariaDBTeamFormHistory struct{ db *sql.DB }

func NewMariaDBTeamFormHistory(db *sql.DB) *MariaDBTeamFormHistory {
	return &MariaDBTeamFormHistory{db: db}
}
func (h *MariaDBTeamFormHistory) FindTeamForm(ctx context.Context, team, format, latest int) ([]TeamFormMatch, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT m.id, COALESCE(m.winner, 0), COALESCE(r.winningMargin, 0), COALESCE(r.winByRuns, 0), m.startdt FROM krik_match_archive m LEFT JOIN stats_import3_dump_matchresult_tbl r ON r.matchId = m.id WHERE (m.teama = ? OR m.teamb = ?) AND m.match_type_id = ? AND m.isArchived = 1 ORDER BY m.startdt DESC LIMIT ?`, team, team, format, latest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := []TeamFormMatch{}
	for rows.Next() {
		var match TeamFormMatch
		if err := rows.Scan(&match.MatchID, &match.Winner, &match.Margin, &match.WonByRuns, &match.PlayedAt); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}
