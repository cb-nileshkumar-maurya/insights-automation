package generation

import (
	"context"
	"errors"
	"sort"
	"time"
)

type TemplateID string

const (
	H2HRecord          TemplateID = "h2h_record"
	TeamForm           TemplateID = "team_form"
	VenueDNA           TemplateID = "venue_dna"
	PlayerStatsAtVenue TemplateID = "player_stats_at_venue"
	Last5Games         TemplateID = "last_5_games"
	TeamPhaseProfiles  TemplateID = "team_phase_profiles"
)

type CardState string

const PreToss CardState = "pre_toss"

type Role string

const (
	Scheduler  Role = "scheduler"
	Editor     Role = "editor"
	Operations Role = "operations"
)

type Caller struct{ Role Role }

type RunMode string

const (
	Normal       RunMode = "normal"
	Regeneration RunMode = "regeneration"
)

type CardTarget struct {
	Template TemplateID `json:"template"`
	CardSet  string     `json:"card_set"`
}

type StartRequest struct {
	Target    CardTarget
	MatchID   string
	CardState CardState
	Inputs    map[string]any
	Caller    Caller
	Mode      RunMode
}

type RunState string

const (
	Queued              RunState = "queued"
	Running             RunState = "running"
	Succeeded           RunState = "succeeded"
	Failed              RunState = "failed"
	CompletedWithErrors RunState = "completed_with_errors"
)

type FailureKind string

const (
	ValidationFailure    FailureKind = "validation"
	ConfigurationFailure FailureKind = "template_configuration"
	SampleFailure        FailureKind = "insufficient_sample"
	TransientFailure     FailureKind = "transient"
)

type Run struct {
	ID               string
	ParentID         string
	Target           CardTarget
	MatchID          string
	CardState        CardState
	NormalizedInputs map[string]any
	State            RunState
	Mode             RunMode
	Attempt          int
	ResultVersion    int
	Failure          *RunFailure
	Children         []string
	CreatedAt        time.Time
	LeaseUntil       time.Time
}

type RunFailure struct {
	Kind    FailureKind
	Message string
}

type ResultEnvelope struct {
	Template          TemplateID
	TemplateVersion   string
	NormalizedFilters map[string]any
	SourceDataWindow  string
	SampleSize        int
	GeneratedAt       time.Time
	ResultVersion     int
	Fallbacks         []string
}

type Result struct {
	Envelope  ResultEnvelope
	Data      map[string]any
	Version   int
	NoContent bool `json:"-"`
}

// LocatedResult is the public state of one result locator.
type LocatedResult struct {
	ResultStatus struct {
		Status RunState `json:"status"`
	} `json:"result_status"`
	Result  *Result     `json:"result,omitempty"`
	Failure *RunFailure `json:"failure,omitempty"`
}

type GeneratedData struct {
	Data             map[string]any
	SampleSize       int
	Fallbacks        []string
	SourceDataWindow string
	NoContent        bool
	Err              *RunFailure
}

type CricketData interface {
	Generate(context.Context, GenerationQuery) GeneratedData
}

type GenerationQuery struct {
	Template TemplateID
	MatchID  string
	Inputs   map[string]any
}

type ScriptedCricketData struct {
	Responses map[TemplateID]GeneratedData
	Queries   []GenerationQuery
}

func (s *ScriptedCricketData) Generate(_ context.Context, query GenerationQuery) GeneratedData {
	s.Queries = append(s.Queries, query)
	if response, ok := s.Responses[query.Template]; ok {
		return response
	}
	return GeneratedData{Err: &RunFailure{Kind: ConfigurationFailure, Message: "no scripted response"}}
}

type Clock interface{ Now() time.Time }
type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time { return f() }

type Time = time.Time

func ParseTime(value string) time.Time { parsed, _ := time.Parse(time.RFC3339, value); return parsed }

var (
	ErrNotFound       = errors.New("not found")
	ErrUnauthorized   = errors.New("regeneration requires an editor or operations caller")
	ErrInvalidRequest = errors.New("invalid generation request")
)

func canonicalInputs(inputs map[string]any) map[string]any {
	copy := make(map[string]any, len(inputs))
	for key, value := range inputs {
		copy[key] = value
	}
	return copy
}
func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
