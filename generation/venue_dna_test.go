package generation

import (
	"context"
	"testing"
)

type fixedVenueHistory struct {
	venue   []VenueMatch
	country []VenueMatch
}

func (history fixedVenueHistory) VenueMatches(context.Context, int, int, string) ([]VenueMatch, error) {
	return history.venue, nil
}
func (history fixedVenueHistory) CountryMatches(context.Context, int, int, string) ([]VenueMatch, error) {
	return history.country, nil
}

func TestVenueDNAUsesVisibleCountryFallback(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	venue, country := make([]VenueMatch, 9), make([]VenueMatch, 10)
	for i := range venue {
		venue[i] = VenueMatch{FirstInningsScore: 150, FirstInningsTeam: 1, Winner: 1}
	}
	for i := range country {
		country[i] = VenueMatch{FirstInningsScore: 160, FirstInningsTeam: 1, Winner: 2}
	}
	module := NewModule(DefaultConfiguration(), NewVenueDNAGenerator(fixedVenueHistory{venue: venue, country: country}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: VenueDNA}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"venue": "1", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	result, err := module.GetCurrentResult(context.Background(), VenueDNA, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 10 || result.Envelope.Fallbacks[0] != "country_level" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestVenueDNAFailsWithoutViableFallback(t *testing.T) {
	now := ParseTime("2026-09-08T10:00:00Z")
	matches := make([]VenueMatch, 9)
	module := NewModule(DefaultConfiguration(), NewVenueDNAGenerator(fixedVenueHistory{venue: matches, country: matches}), NewMemoryRunStore(), ClockFunc(func() Time { return now }))
	run, err := module.StartRun(context.Background(), StartRequest{Target: CardTarget{Template: VenueDNA}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"venue": "1", "format": "t20"}})
	if err != nil {
		t.Fatal(err)
	}
	module.runAll(context.Background())
	failed, _ := module.GetRun(context.Background(), run.ID)
	if failed.State != Failed || failed.Failure.Kind != SampleFailure {
		t.Fatalf("run=%#v", failed)
	}
}
