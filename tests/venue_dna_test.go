package tests

import (
	"context"
	"testing"

	. "github.com/cricbuzz/insights-automation/generation"
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
	venue, country := make([]VenueMatch, 9), make([]VenueMatch, 10)
	for i := range venue {
		venue[i] = VenueMatch{FirstInningsScore: 150, FirstInningsTeam: 1, Winner: 1}
	}
	for i := range country {
		country[i] = VenueMatch{FirstInningsScore: 160, FirstInningsTeam: 1, Winner: 2}
	}
	harness := newGenerationHarness(t, NewVenueDNAGenerator(fixedVenueHistory{venue: venue, country: country}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: VenueDNA}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"venue": "1", "format": "t20"}})
	result, err := harness.module.GetCurrentResult(harness.ctx, VenueDNA, "m-1", PreToss, run.NormalizedInputs)
	if err != nil || result.Envelope.SampleSize != 10 || result.Envelope.Fallbacks[0] != "country_level" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestVenueDNAFailsWithoutViableFallback(t *testing.T) {
	matches := make([]VenueMatch, 9)
	harness := newGenerationHarness(t, NewVenueDNAGenerator(fixedVenueHistory{venue: matches, country: matches}))
	run := harness.startAndProcess(t, StartRequest{Target: CardTarget{Template: VenueDNA}, MatchID: "m-1", CardState: PreToss, Inputs: map[string]any{"venue": "1", "format": "t20"}})
	failed, err := harness.module.GetRun(harness.ctx, run.ID)
	if err != nil || failed.State != Failed || failed.Failure.Kind != SampleFailure {
		t.Fatalf("run=%#v err=%v", failed, err)
	}
}
