package generation

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type H2HMatchRecord struct {
	MatchID    int
	Team1ID    int
	Team1Name  string
	Team2ID    int
	Team2Name  string
	Winner     int
	ResultType string
	Margin     int
	WonByRuns  bool
	PlayedAt   time.Time
}
type H2HHistory interface {
	FindH2H(context.Context, int, int, int, int) ([]H2HMatchRecord, error)
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
	matches, err := g.history.FindH2H(ctx, teamA, teamB, format, latest)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	if len(matches) == 0 {
		return GeneratedData{Err: &RunFailure{Kind: SampleFailure, Message: "no comparable H2H matches"}}
	}
	wins := map[int]int{}
	names := map[int]string{}
	draws, noResults := 0, 0
	records := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		names[match.Team1ID], names[match.Team2ID] = match.Team1Name, match.Team2Name
		record := map[string]any{"match_id": match.MatchID, "start_date": match.PlayedAt.Format("2006-01-02"), "team1_id": match.Team1ID, "team2_id": match.Team2ID, "winner_team_id": nil, "margin": nil}
		if match.Winner == teamA || match.Winner == teamB {
			wins[match.Winner]++
			record["outcome"] = "win"
			record["winner_team_id"] = match.Winner
			record["margin"] = map[string]any{"value": match.Margin, "type": marginType(match.WonByRuns)}
			record["result_string"] = resultString(names[match.Winner], match.Margin, match.WonByRuns)
		} else if strings.EqualFold(match.ResultType, "draw") {
			draws++
			record["outcome"] = "draw"
			record["result_string"] = "Match drawn"
		} else {
			noResults++
			record["outcome"] = "no_result"
			record["result_string"] = "No result"
		}
		records = append(records, record)
	}
	data := map[string]any{"summary": map[string]any{"matches_played": len(matches), "draws": draws, "no_results": noResults, "teams": []map[string]any{teamSummary(teamA, names[teamA], wins[teamA], wins[teamB], len(matches)), teamSummary(teamB, names[teamB], wins[teamB], wins[teamA], len(matches))}}, "head_to_head": records}
	fallbacks := []string{}
	if len(matches) < latest {
		fallbacks = append(fallbacks, "available_history")
	}
	return GeneratedData{Data: data, SampleSize: len(matches), Fallbacks: fallbacks, SourceDataWindow: matches[len(matches)-1].PlayedAt.Format("2006-01-02") + ".." + matches[0].PlayedAt.Format("2006-01-02")}
}
func invalidH2H(err error) GeneratedData {
	return GeneratedData{Err: &RunFailure{Kind: ValidationFailure, Message: err.Error()}}
}
func teamSummary(id int, name string, wins, losses, total int) map[string]any {
	return map[string]any{"team_id": id, "team_name": name, "wins": wins, "losses": losses, "win_rate": float64(wins) * 100 / float64(total)}
}
func marginType(wonByRuns bool) string {
	if wonByRuns {
		return "runs"
	}
	return "wickets"
}
func resultString(winner string, margin int, wonByRuns bool) string {
	unit := strings.TrimSuffix(marginType(wonByRuns), "s")
	if margin != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%s won by %d %s", winner, margin, unit)
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
func (h *MariaDBH2HHistory) FindH2H(ctx context.Context, teamA, teamB, format, latest int) ([]H2HMatchRecord, error) {
	args := []any{teamA, teamB, teamB, teamA, format}
	args = append(args, latest)
	rows, err := h.db.QueryContext(ctx, `SELECT m.id, m.teama, COALESCE(team1.name, ''), m.teamb, COALESCE(team2.name, ''), COALESCE(r.winningTeamId, m.winner, 0), COALESCE(r.resultType, ''), COALESCE(r.winningMargin, 0), COALESCE(r.winByRuns, 0), m.startdt FROM krik_match_archive m LEFT JOIN krik_teams team1 ON team1.id = m.teama LEFT JOIN krik_teams team2 ON team2.id = m.teamb LEFT JOIN stats_import3_dump_matchresult_tbl r ON r.matchId = m.id WHERE ((m.teama = ? AND m.teamb = ?) OR (m.teama = ? AND m.teamb = ?)) AND m.match_type_id = ? AND m.isArchived = 1 ORDER BY m.startdt DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []H2HMatchRecord{}
	for rows.Next() {
		var result H2HMatchRecord
		if err := rows.Scan(&result.MatchID, &result.Team1ID, &result.Team1Name, &result.Team2ID, &result.Team2Name, &result.Winner, &result.ResultType, &result.Margin, &result.WonByRuns, &result.PlayedAt); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
