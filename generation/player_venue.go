package generation

import (
	"context"
	"fmt"
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
	players, err := playerIDs(query.Inputs["players"])
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
	batting, bowling, err := g.history.PlayerVenueStats(ctx, players, venue, format, window, false)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	careerBatting, careerBowling, err := g.history.PlayerVenueStats(ctx, players, venue, format, "career", true)
	if err != nil {
		return GeneratedData{Err: &RunFailure{Kind: TransientFailure, Message: err.Error()}}
	}
	entries, fallbacks, sample := make([]any, 0, len(players)), []string{}, 0
	for _, player := range players {
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
		entries = append(entries, map[string]any{"player": player, "batting": bat, "bowling": bowl, "batting_fallback": batFallback, "bowling_fallback": bowlFallback})
	}
	return GeneratedData{Data: map[string]any{"players": entries}, SampleSize: sample, Fallbacks: fallbacks, SourceDataWindow: window}
}
func playerIDs(raw any) ([]int, error) {
	values, ok := raw.([]string)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("players must be a non-empty stable ID list")
	}
	ids := make([]int, 0, len(values))
	for _, value := range values {
		id, err := inputID(map[string]any{"player": value}, "player")
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
