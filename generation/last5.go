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
	runs := 0
	for _, game := range games {
		runs += game.BattingRuns
	}
	average := float64(runs) / float64(len(games))
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

func OpenMariaDBLast5GamesGenerator(ctx context.Context) (*Last5GamesGenerator, *sql.DB, error) {
	db, err := OpenReadReplica(ctx)
	if err != nil {
		return nil, nil, err
	}
	return NewLast5GamesGenerator(NewMariaDBLast5History(db)), db, nil
}

func (h *MariaDBLast5History) LastGames(ctx context.Context, players []int, format, latest int) (map[int][]PlayerGame, map[int]PlayerCareerComparison, error) {
	marks, ids := playerPlaceholders(players)
	args := append(ids, format, latest)
	rows, err := h.db.QueryContext(ctx, `WITH ranked AS (SELECT b.playerId, b.matchId, b.runs, b.balls, ROW_NUMBER() OVER (PARTITION BY b.playerId ORDER BY m.startdt DESC) AS position FROM stats_import3_dump_battingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.playerId IN (`+marks+`) AND m.match_type_id = ?) SELECT playerId, matchId, runs, balls FROM ranked WHERE position <= ?`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	games := map[int][]PlayerGame{}
	for rows.Next() {
		var player int
		var game PlayerGame
		if err := rows.Scan(&player, &game.MatchID, &game.BattingRuns, &game.BattingBalls); err != nil {
			return nil, nil, err
		}
		games[player] = append(games[player], game)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	bowlRows, err := h.db.QueryContext(ctx, `WITH ranked AS (SELECT b.bowlerId, b.matchId, b.wickets, b.runsGiven, ROW_NUMBER() OVER (PARTITION BY b.bowlerId ORDER BY m.startdt DESC) AS position FROM stats_import3_dump_bowlingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.bowlerId IN (`+marks+`) AND m.match_type_id = ?) SELECT bowlerId, matchId, wickets, runsGiven FROM ranked WHERE position <= ?`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer bowlRows.Close()
	for bowlRows.Next() {
		var player int
		var matchID, wickets, runs int
		if err := bowlRows.Scan(&player, &matchID, &wickets, &runs); err != nil {
			return nil, nil, err
		}
		merged := false
		for index := range games[player] {
			if games[player][index].MatchID == matchID {
				games[player][index].BowlingWickets, games[player][index].BowlingRuns, merged = wickets, runs, true
				break
			}
		}
		if !merged {
			games[player] = append(games[player], PlayerGame{MatchID: matchID, BowlingWickets: wickets, BowlingRuns: runs})
		}
	}
	if err := bowlRows.Err(); err != nil {
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
