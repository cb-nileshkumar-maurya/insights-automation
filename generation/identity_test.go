package generation

import "testing"

func TestInputHashIsStableAcrossFilterOrderAndChangesWithMatchContext(t *testing.T) {
	first, err := InputHash(H2HRecord, "v1", "m-1", PreToss, map[string]any{"format": "t20", "team_a": "india", "team_b": "australia"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := InputHash(H2HRecord, "v1", "m-1", PreToss, map[string]any{"team_b": "australia", "team_a": "india", "format": "t20"})
	if err != nil {
		t.Fatal(err)
	}
	differentMatch, err := InputHash(H2HRecord, "v1", "m-2", PreToss, map[string]any{"format": "t20", "team_a": "india", "team_b": "australia"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first == differentMatch {
		t.Fatalf("hashes: first=%q second=%q different=%q", first, second, differentMatch)
	}
}
