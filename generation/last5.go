package generation

import (
	"context"
	"database/sql"
)

type PlayerGame struct {
	MatchID                                                int
	BattingRuns, BattingBalls, BowlingWickets, BowlingRuns int
}
type PlayerCareerComparison struct{ BattingAverage, BowlingWicketsPerGame float64 }
type Last5History interface {
	LastGames(context.Context, []int, int, int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error)
}
type Last5GamesGenerator struct{ history Last5History }

func NewLast5GamesGenerator(history Last5History) *Last5GamesGenerator {
	return &Last5GamesGenerator{history: history}
}
func (g *Last5GamesGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != Last5Games {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "Last 5 Games generator received another template"}}
	}
	players, err := playerIDs(query.Inputs["players"])
	if err != nil {
		return invalidH2H(err)
	}
	format, err := formatID(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	latest := query.Inputs["latest_matches"].(int)
	games, careers, err := g.history.LastGames(ctx, players, format, latest)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	entries, fallbacks, sample := make([]any, 0, len(players)), []string{}, 0
	for _, player := range players {
		playerGames := games[player]
		sample += len(playerGames)
		if len(playerGames) < latest {
			fallbacks = append(fallbacks, "player_available_history")
		}
		entries = append(entries, map[string]any{"player": player, "games": playerGames, "career_comparison": careers[player], "form_arrow": formArrow(playerGames, careers[player]), "available_history": len(playerGames), "insufficient_sample": len(playerGames) == 0})
	}
	return GeneratedData{Data: map[string]any{"players": entries}, SampleSize: sample, Fallbacks: fallbacks, SourceDataWindow: "latest games"}
}
func formArrow(games []PlayerGame, career PlayerCareerComparison) string {
	if len(games) == 0 {
		return "unavailable"
	}
	runs, wickets, battingAppearances := 0, 0, 0
	for _, game := range games {
		runs += game.BattingRuns
		wickets += game.BowlingWickets
		if game.BattingBalls > 0 {
			battingAppearances++
		}
	}
	if battingAppearances == 0 {
		average := float64(wickets) / float64(len(games))
		if average > career.BowlingWicketsPerGame {
			return "up"
		}
		if average < career.BowlingWicketsPerGame {
			return "down"
		}
		return "level"
	}
	average := float64(runs) / float64(battingAppearances)
	if average > career.BattingAverage {
		return "up"
	}
	if average < career.BattingAverage {
		return "down"
	}
	return "level"
}

type MariaDBLast5History struct{ db *sql.DB }

func NewMariaDBLast5History(db *sql.DB) *MariaDBLast5History { return &MariaDBLast5History{db: db} }

func (h *MariaDBLast5History) LastGames(ctx context.Context, players []int, format, latest int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error) {
	marks, ids := playerPlaceholders(players)
	recentArgs := append([]any{}, ids...)
	recentArgs = append(recentArgs, format)
	recentArgs = append(recentArgs, ids...)
	recentArgs = append(recentArgs, format, latest)
	rows, err := h.db.QueryContext(ctx, `WITH activity AS (SELECT b.playerId AS player_id, b.matchId AS match_id, m.startdt FROM stats_import3_dump_battingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.playerId IN (`+marks+`) AND m.match_type_id = ? UNION SELECT b.bowlerId, b.matchId, m.startdt FROM stats_import3_dump_bowlingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.bowlerId IN (`+marks+`) AND m.match_type_id = ?), ranked AS (SELECT player_id, match_id, startdt, ROW_NUMBER() OVER (PARTITION BY player_id ORDER BY startdt DESC, match_id DESC) AS position FROM activity) SELECT r.player_id, r.match_id, COALESCE(SUM(bat.runs),0), COALESCE(SUM(bat.balls),0), COALESCE(SUM(bowl.wickets),0), COALESCE(SUM(bowl.runsGiven),0) FROM ranked r LEFT JOIN stats_import3_dump_battingcard_tbl bat ON bat.playerId = r.player_id AND bat.matchId = r.match_id LEFT JOIN stats_import3_dump_bowlingcard_tbl bowl ON bowl.bowlerId = r.player_id AND bowl.matchId = r.match_id WHERE r.position <= ? GROUP BY r.player_id, r.match_id, r.startdt ORDER BY r.player_id, r.startdt DESC, r.match_id DESC`, recentArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	games := map[int][]PlayerGame{}
	for rows.Next() {
		var player int
		var game PlayerGame
		if err := rows.Scan(&player, &game.MatchID, &game.BattingRuns, &game.BattingBalls, &game.BowlingWickets, &game.BowlingRuns); err != nil {
			return nil, nil, err
		}
		games[player] = append(games[player], game)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	careerRows, err := h.db.QueryContext(ctx, `SELECT b.playerId, AVG(b.runs) FROM stats_import3_dump_battingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.playerId IN (`+marks+`) AND m.match_type_id = ? GROUP BY b.playerId`, append(ids, format)...)
	if err != nil {
		return nil, nil, err
	}
	defer careerRows.Close()
	careers := map[int]PlayerCareerComparison{}
	for careerRows.Next() {
		var player int
		var average float64
		if err := careerRows.Scan(&player, &average); err != nil {
			return nil, nil, err
		}
		careers[player] = PlayerCareerComparison{BattingAverage: average}
	}
	if err := careerRows.Err(); err != nil {
		return nil, nil, err
	}
	bowlingCareerRows, err := h.db.QueryContext(ctx, `SELECT b.bowlerId, AVG(b.wickets) FROM stats_import3_dump_bowlingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.bowlerId IN (`+marks+`) AND m.match_type_id = ? GROUP BY b.bowlerId`, append(ids, format)...)
	if err != nil {
		return nil, nil, err
	}
	defer bowlingCareerRows.Close()
	for bowlingCareerRows.Next() {
		var player int
		var average float64
		if err := bowlingCareerRows.Scan(&player, &average); err != nil {
			return nil, nil, err
		}
		career := careers[player]
		career.BowlingWicketsPerGame = average
		careers[player] = career
	}
	if err := bowlingCareerRows.Err(); err != nil {
		return nil, nil, err
	}
	return games, careers, nil
}
