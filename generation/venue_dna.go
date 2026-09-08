package generation

import (
	"context"
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
	data := map[string]any{"average_first_innings_score": float64(firstTotal) / float64(len(matches)), "bat_first_win_rate": float64(batFirstWins) * 100 / float64(len(matches)), "chase_win_rate": float64(chaseWins) * 100 / float64(len(matches)), "high_total": high, "low_total": low, "match_phase_benchmarks": map[string]any{}}
	return GeneratedData{Data: data, SampleSize: len(matches), Fallbacks: fallbacks, SourceDataWindow: fmt.Sprintf("%s window", window)}
}
