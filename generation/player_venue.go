package generation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const playerVenueMinimumInnings = 15

type PlayerVenueDiscipline struct{ Innings, Runs, Balls, Wickets, RunsConceded int }
type PlayerVenueHistory interface {
	PlayerVenueStats(context.Context, []int, int, int, string, bool) (map[int]PlayerVenueDiscipline, map[int]PlayerVenueDiscipline, error)
}
type PlayerVenueGenerator struct{ history PlayerVenueHistory }

func NewPlayerVenueGenerator(history PlayerVenueHistory) *PlayerVenueGenerator {
	return &PlayerVenueGenerator{history: history}
}
func (g *PlayerVenueGenerator) Generate(ctx context.Context, query GenerationQuery) GeneratedData {
	if query.Template != PlayerStatsAtVenue {
		return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "Player Stats at Venue generator received another template"}}
	}
	population, err := playerPopulationFrom(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	venue, err := inputID(query.Inputs, "venue")
	if err != nil {
		return invalidH2H(err)
	}
	format, err := formatID(query.Inputs)
	if err != nil {
		return invalidH2H(err)
	}
	window := query.Inputs["window"].(string)
	batting, bowling, err := g.history.PlayerVenueStats(ctx, population.ids, venue, format, window, false)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	careerBatting, careerBowling, err := g.history.PlayerVenueStats(ctx, population.ids, venue, format, "career", true)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	entries, fallbacks, sample := make([]any, 0, len(population.ids)), []string{}, 0
	for _, player := range population.ids {
		bat := batting[player]
		bowl := bowling[player]
		batFallback, bowlFallback := false, false
		if bat.Innings < playerVenueMinimumInnings {
			bat = careerBatting[player]
			batFallback = true
		}
		if bowl.Innings < playerVenueMinimumInnings {
			bowl = careerBowling[player]
			bowlFallback = true
		}
		sample += bat.Innings + bowl.Innings
		if batFallback {
			fallbacks = append(fallbacks, fmt.Sprintf("player_%d_batting_career", player))
		}
		if bowlFallback {
			fallbacks = append(fallbacks, fmt.Sprintf("player_%d_bowling_career", player))
		}
		identity := population.identities[player]
		entries = append(entries, map[string]any{"player_id": player, "team_id": identity.TeamID, "player_name": identity.FullName, "batting": bat, "bowling": bowl, "batting_fallback": batFallback, "bowling_fallback": bowlFallback})
	}
	return GeneratedData{Data: map[string]any{"players": entries}, SampleSize: sample, Fallbacks: fallbacks, SourceDataWindow: window}
}

type MariaDBPlayerVenueHistory struct{ db *sql.DB }

func NewMariaDBPlayerVenueHistory(db *sql.DB) *MariaDBPlayerVenueHistory {
	return &MariaDBPlayerVenueHistory{db: db}
}
func (h *MariaDBPlayerVenueHistory) PlayerVenueStats(ctx context.Context, players []int, venue, format int, window string, career bool) (map[int]PlayerVenueDiscipline, map[int]PlayerVenueDiscipline, error) {
	marks, args := playerPlaceholders(players)
	filter := "m.match_type_id = ?"
	args = append(args, format)
	if !career {
		filter += " AND m.venueid = ?"
		args = append(args, venue)
	}
	if window == "since_2024" && !career {
		filter += " AND m.startdt >= '2024-01-01'"
	}
	batting, err := h.read(ctx, `SELECT b.playerId, COUNT(*), COALESCE(SUM(b.runs),0), COALESCE(SUM(b.balls),0), 0, 0 FROM stats_import3_dump_battingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.playerId IN (`+marks+`) AND `+filter+` GROUP BY b.playerId`, args...)
	if err != nil {
		return nil, nil, err
	}
	bowling, err := h.read(ctx, `SELECT b.bowlerId, COUNT(*), 0, 0, COALESCE(SUM(b.wickets),0), COALESCE(SUM(b.runsGiven),0) FROM stats_import3_dump_bowlingcard_tbl b JOIN krik_match_archive m ON m.id = b.matchId WHERE b.bowlerId IN (`+marks+`) AND `+filter+` GROUP BY b.bowlerId`, args...)
	if err != nil {
		return nil, nil, err
	}
	return batting, bowling, nil
}
func (h *MariaDBPlayerVenueHistory) read(ctx context.Context, statement string, args ...any) (map[int]PlayerVenueDiscipline, error) {
	rows, err := h.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[int]PlayerVenueDiscipline{}
	for rows.Next() {
		var player int
		var stat PlayerVenueDiscipline
		if err := rows.Scan(&player, &stat.Innings, &stat.Runs, &stat.Balls, &stat.Wickets, &stat.RunsConceded); err != nil {
			return nil, err
		}
		values[player] = stat
	}
	return values, rows.Err()
}
func playerPlaceholders(players []int) (string, []any) {
	marks, args := make([]string, len(players)), make([]any, len(players))
	for index, player := range players {
		marks[index], args[index] = "?", player
	}
	return strings.Join(marks, ","), args
}
