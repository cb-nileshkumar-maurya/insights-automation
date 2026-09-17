package generation

type Template struct {
	ID       TemplateID
	Version  string
	Allowed  map[string][]any
	Required []string
	Defaults map[string]any
	FreshFor int
}

type Configuration struct {
	Templates map[TemplateID]Template
	CardSets  map[string][]TemplateID
}

func DefaultConfiguration() Configuration {
	formats := []any{"t20", "odi"}
	latest := []any{5, 10}
	h2hLatest := []any{5, 10, 15, 20}
	window := []any{"since_2024", "career"}
	return Configuration{Templates: map[TemplateID]Template{
		H2HRecord:          {ID: H2HRecord, Version: "v3", Required: []string{"team_a", "team_b", "format"}, Allowed: map[string][]any{"team_a": {}, "team_b": {}, "format": formats, "latest_matches": h2hLatest, "team_form_latest_matches": latest, "venue": {}, "host_country": {}, "target_start": {}}, Defaults: map[string]any{"latest_matches": 10, "team_form_latest_matches": 5}, FreshFor: 15},
		TeamForm:           {ID: TeamForm, Version: "v1", Required: []string{"team_a", "team_b", "format"}, Allowed: map[string][]any{"team_a": {}, "team_b": {}, "format": formats, "latest_matches": latest}, Defaults: map[string]any{"latest_matches": 5}, FreshFor: 15},
		VenueDNA:           {ID: VenueDNA, Version: "v1", Required: []string{"venue", "format"}, Allowed: map[string][]any{"venue": {}, "format": formats, "window": window}, Defaults: map[string]any{"window": "since_2024"}, FreshFor: 60},
		PlayerStatsAtVenue: {ID: PlayerStatsAtVenue, Version: "v2", Required: []string{"players", "venue", "format"}, Allowed: map[string][]any{"players": {}, "venue": {}, "format": formats, "window": window, "player_context": {}}, Defaults: map[string]any{"window": "since_2024"}, FreshFor: 60},
		Last5Games:         {ID: Last5Games, Version: "v2", Required: []string{"players", "format"}, Allowed: map[string][]any{"players": {}, "format": formats, "latest_matches": latest, "player_context": {}}, Defaults: map[string]any{"latest_matches": 5}, FreshFor: 15},
		TeamPhaseProfiles:  {ID: TeamPhaseProfiles, Version: "v1", Required: []string{"team_a", "team_b", "format"}, Allowed: map[string][]any{"team_a": {}, "team_b": {}, "format": formats, "window": window}, Defaults: map[string]any{"window": "since_2024"}, FreshFor: 60},
	}, CardSets: map[string][]TemplateID{"pre_toss_v1": {H2HRecord, TeamForm, VenueDNA, PlayerStatsAtVenue, Last5Games, TeamPhaseProfiles}}}
}
