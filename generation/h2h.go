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
	MatchID            int
	Team1ID            int
	Team1Name          string
	Team2ID            int
	Team2Name          string
	Winner             int
	ResultType         string
	Margin             int
	WonByRuns          bool
	PlayedAt           time.Time
	BattingFirstTeamID int
	ChasingTeamID      int
}
type H2HHistory interface {
	FindH2H(context.Context, int, int, int, int) ([]H2HMatchRecord, error)
}
type H2HScope struct {
	Venue, Country int
	Before         time.Time
}
type CompositeH2HHistory interface {
	FindH2HScoped(context.Context, int, int, int, int, H2HScope) ([]H2HMatchRecord, error)
	FindTeamFormScoped(context.Context, int, int, int, H2HScope) ([]H2HMatchRecord, error)
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
	if history, ok := g.history.(CompositeH2HHistory); ok {
		return g.generateComposite(ctx, query, history, teamA, teamB, format, latest)
	}
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

func (g *H2HGenerator) generateComposite(ctx context.Context, query GenerationQuery, history CompositeH2HHistory, teamA, teamB, format, latest int) GeneratedData {
	before, err := time.Parse(time.RFC3339, fmt.Sprint(query.Inputs["target_start"]))
	if err != nil {
		return invalidH2H(fmt.Errorf("target_start is required"))
	}
	formLatest := 5
	if value, ok := query.Inputs["team_form_latest_matches"].(int); ok {
		formLatest = value
	}
	overall := H2HScope{Before: before}
	h2h, err := history.FindH2HScoped(ctx, teamA, teamB, format, latest, overall)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	formA, err := history.FindTeamFormScoped(ctx, teamA, format, formLatest, overall)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	formB, err := history.FindTeamFormScoped(ctx, teamB, format, formLatest, overall)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	if len(h2h)+len(formA)+len(formB) == 0 {
		return GeneratedData{NoContent: true}
	}
	h2hData := map[string]any{"overall": h2hSlice(h2h, teamA, teamB, latest)}
	formData := map[string]any{"overall": teamFormSlice(formA, formB, teamA, teamB, formLatest)}
	if venue, _ := query.Inputs["venue"].(int); venue > 0 {
		scope := H2HScope{Venue: venue, Before: before}
		if matches, err := history.FindH2HScoped(ctx, teamA, teamB, format, latest, scope); err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		} else {
			h2hData["at_venue"] = h2hSlice(matches, teamA, teamB, latest)
		}
		a, err := history.FindTeamFormScoped(ctx, teamA, format, formLatest, scope)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
		b, err := history.FindTeamFormScoped(ctx, teamB, format, formLatest, scope)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
		formData["at_venue"] = teamFormSlice(a, b, teamA, teamB, formLatest)
	}
	if country, _ := query.Inputs["host_country"].(int); country > 0 {
		scope := H2HScope{Country: country, Before: before}
		a, err := history.FindTeamFormScoped(ctx, teamA, format, formLatest, scope)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
		b, err := history.FindTeamFormScoped(ctx, teamB, format, formLatest, scope)
		if err != nil {
			return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
		}
		formData["in_host_country"] = teamFormSlice(a, b, teamA, teamB, formLatest)
	}
	return GeneratedData{Data: map[string]any{"head_to_head": h2hData, "team_form": formData}, SampleSize: len(h2h) + len(formA) + len(formB)}
}

func h2hSlice(matches []H2HMatchRecord, teamA, teamB, requested int) map[string]any {
	wins, draws, noResults := map[int]int{}, 0, 0
	names := map[int]string{}
	results := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		names[match.Team1ID], names[match.Team2ID] = match.Team1Name, match.Team2Name
		result := map[string]any{"match_id": match.MatchID, "start_date": match.PlayedAt.Format("2006-01-02"), "team1_id": match.Team1ID, "team2_id": match.Team2ID, "winner_team_id": nil, "margin": nil, "batting_first_team_id": nullableID(match.BattingFirstTeamID), "chasing_team_id": nullableID(match.ChasingTeamID)}
		if match.Winner == teamA || match.Winner == teamB {
			wins[match.Winner]++
			result["outcome"] = "win"
			result["winner_team_id"] = match.Winner
			result["margin"] = map[string]any{"value": match.Margin, "type": marginType(match.WonByRuns)}
			result["result_string"] = resultString(names[match.Winner], match.Margin, match.WonByRuns)
		} else if strings.EqualFold(match.ResultType, "draw") {
			draws++
			result["outcome"] = "draw"
			result["result_string"] = "Match drawn"
		} else {
			noResults++
			result["outcome"] = "no_result"
			result["result_string"] = "No result"
		}
		results = append(results, result)
	}
	return map[string]any{"requested_history": requested, "available_history": len(matches), "insufficient_sample": len(matches) < requested, "summary": map[string]any{"matches_played": len(matches), "draws": draws, "no_results": noResults, "teams": []map[string]any{teamSummary(teamA, names[teamA], wins[teamA], wins[teamB], len(matches)), teamSummary(teamB, names[teamB], wins[teamB], wins[teamA], len(matches))}}, "results": results}
}

func teamFormSlice(a, b []H2HMatchRecord, teamA, teamB, requested int) map[string]any {
	return map[string]any{"requested_history": requested, "teams": []map[string]any{teamFormResult(teamA, a, requested), teamFormResult(teamB, b, requested)}}
}

func teamFormResult(team int, matches []H2HMatchRecord, requested int) map[string]any {
	name := ""
	results := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		opponent, opponentName := match.Team1ID, match.Team1Name
		if opponent == team {
			opponent, opponentName = match.Team2ID, match.Team2Name
		}
		if match.Team1ID == team {
			name = match.Team1Name
		} else {
			name = match.Team2Name
		}
		outcome, result := "no_result", map[string]any{"match_id": match.MatchID, "start_date": match.PlayedAt.Format("2006-01-02"), "opposition_id": opponent, "opposition_name": opponentName, "winner_team_id": nil, "margin": nil, "batting_first_team_id": nullableID(match.BattingFirstTeamID), "chasing_team_id": nullableID(match.ChasingTeamID)}
		if match.Winner == team {
			outcome = "win"
		} else if match.Winner != 0 {
			outcome = "loss"
		} else if strings.EqualFold(match.ResultType, "draw") {
			outcome = "draw"
		}
		result["outcome"] = outcome
		if match.Winner != 0 {
			result["winner_team_id"] = match.Winner
			result["margin"] = map[string]any{"value": match.Margin, "type": marginType(match.WonByRuns)}
			winner := match.Team1Name
			if match.Winner == match.Team2ID {
				winner = match.Team2Name
			}
			result["result_string"] = resultString(winner, match.Margin, match.WonByRuns)
		} else if outcome == "draw" {
			result["result_string"] = "Match drawn"
		} else {
			result["result_string"] = "No result"
		}
		results = append(results, result)
	}
	return map[string]any{"team_id": team, "team_name": name, "available_history": len(matches), "insufficient_sample": len(matches) < requested, "results": results}
}

func nullableID(id int) any {
	if id == 0 {
		return nil
	}
	return id
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

func (h *MariaDBH2HHistory) FindH2HScoped(ctx context.Context, teamA, teamB, format, latest int, scope H2HScope) ([]H2HMatchRecord, error) {
	return h.findScoped(ctx, "((m.teama = ? AND m.teamb = ?) OR (m.teama = ? AND m.teamb = ?))", []any{teamA, teamB, teamB, teamA}, format, latest, scope)
}
func (h *MariaDBH2HHistory) FindTeamFormScoped(ctx context.Context, team, format, latest int, scope H2HScope) ([]H2HMatchRecord, error) {
	return h.findScoped(ctx, "(m.teama = ? OR m.teamb = ?)", []any{team, team}, format, latest, scope)
}
func (h *MariaDBH2HHistory) findScoped(ctx context.Context, population string, args []any, format, latest int, scope H2HScope) ([]H2HMatchRecord, error) {
	if scope.Venue > 0 {
		population += " AND m.venueid = ?"
		args = append(args, scope.Venue)
	}
	if scope.Country > 0 {
		population += " AND venue.country_id = ?"
		args = append(args, scope.Country)
	}
	args = append(args, format, scope.Before, latest)
	rows, err := h.db.QueryContext(ctx, `SELECT m.id, m.teama, COALESCE(team1.name, ''), m.teamb, COALESCE(team2.name, ''), COALESCE(r.winningTeamId, m.winner, 0), COALESCE(r.resultType, ''), COALESCE(r.winningMargin, 0), COALESCE(r.winByRuns, 0), m.startdt, COALESCE(i.battingTeamId, 0) FROM krik_match_archive m LEFT JOIN krik_teams team1 ON team1.id = m.teama LEFT JOIN krik_teams team2 ON team2.id = m.teamb LEFT JOIN krik_match_venue venue ON venue.id = m.venueid LEFT JOIN stats_import3_dump_matchresult_tbl r ON r.matchId = m.id LEFT JOIN stats_import3_dump_innings_tbl i ON i.matchId = m.id AND i.inningsId = 1 WHERE `+population+` AND m.match_type_id = ? AND m.isArchived = 1 AND m.startdt < ? ORDER BY m.startdt DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []H2HMatchRecord{}
	for rows.Next() {
		var result H2HMatchRecord
		if err := rows.Scan(&result.MatchID, &result.Team1ID, &result.Team1Name, &result.Team2ID, &result.Team2Name, &result.Winner, &result.ResultType, &result.Margin, &result.WonByRuns, &result.PlayedAt, &result.BattingFirstTeamID); err != nil {
			return nil, err
		}
		if result.BattingFirstTeamID == result.Team1ID {
			result.ChasingTeamID = result.Team2ID
		} else if result.BattingFirstTeamID == result.Team2ID {
			result.ChasingTeamID = result.Team1ID
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
