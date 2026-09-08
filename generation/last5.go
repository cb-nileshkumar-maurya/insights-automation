package generation

import "context"

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
