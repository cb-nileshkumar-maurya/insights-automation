// Package app adapts the generation module into a runnable HTTP service.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cricbuzz/insights-automation/generation"
)

// Config supplies host-owned dependencies. Historical reads and match-context
// resolution stay outside the HTTP request so clients cannot choose them.
type Config struct {
	WriteDatabase generation.DatabaseConfig
	Data          generation.CricketData
	Eligible      generation.EligibleMatchSource
	MatchContext  generation.MatchContextResolver
	RoleResolver  RoleResolver
	WorkerPoll    time.Duration
	WorkerID      string
	Ready         func(context.Context) error
}

// Service owns the durable run queue and exposes its submission and result APIs.
type Service struct {
	store        *generation.SQLRunStore
	module       *generation.SQLModule
	worker       *generation.SQLWorker
	reconciler   *generation.Reconciler
	workerPoll   time.Duration
	roleResolver RoleResolver
	ready        func(context.Context) error
	matchContext generation.MatchContextResolver
	config       generation.Configuration
}

// RoleResolver is the command's authentication boundary. Production callers
// receive a role from an authenticated adapter, never from request JSON.
type RoleResolver func(*http.Request) (generation.Role, error)

// New creates a service and applies the SQLite schema only for local stores.
// Production schema ownership remains with the production database deployment.
func New(ctx context.Context, config Config) (*Service, error) {
	store, err := generation.OpenSQLRunStore(ctx, config.WriteDatabase)
	if err != nil {
		return nil, err
	}
	if config.WriteDatabase.Dialect == generation.SQLite {
		if err := store.Migrate(ctx); err != nil {
			store.Close()
			return nil, err
		}
	}
	historicalSourceConfigured := config.Data != nil
	if config.Data == nil {
		// SQLite local mode exercises durable run handling without silently
		// inventing historical cricket data.
		config.Data = unavailableData{}
	}
	if config.WorkerPoll <= 0 {
		config.WorkerPoll = 100 * time.Millisecond
	}
	if config.WorkerID == "" {
		config.WorkerID = "insights-automation"
	}
	if config.RoleResolver == nil {
		config.RoleResolver = localRole
	}
	generationConfig := generation.DefaultConfiguration()
	module := generation.NewSQLModule(generationConfig, store, generation.ClockFunc(func() time.Time { return time.Now().UTC() }))
	if config.Eligible == nil {
		// Match discovery is injected because the authoritative upcoming-match
		// and squad population source is deployment-specific.
		config.Eligible = emptyEligibleMatches{}
	}
	if config.MatchContext == nil {
		config.MatchContext = unavailableMatchContext{}
	}
	externalReady := config.Ready
	config.Ready = func(readyContext context.Context) error {
		if err := store.Ping(readyContext); err != nil {
			return err
		}
		if externalReady != nil {
			return externalReady(readyContext)
		}
		return nil
	}
	slog.Info("insights automation initialized", "write_dialect", config.WriteDatabase.Dialect, "historical_source_configured", historicalSourceConfigured, "worker_id", config.WorkerID)
	return &Service{store: store, module: module, worker: generation.NewSQLWorker(store, config.Data, generationConfig, config.WorkerID), reconciler: generation.NewReconciler(config.Eligible, module, "pre_toss_v1"), workerPoll: config.WorkerPoll, roleResolver: config.RoleResolver, ready: config.Ready, matchContext: config.MatchContext, config: generationConfig}, nil
}

func (s *Service) Close() error { return s.store.Close() }

// Run processes durable child generation runs until the root context ends.
func (s *Service) Run(ctx context.Context) error {
	go func() { _ = s.reconciler.Run(ctx) }()
	ticker := time.NewTicker(s.workerPoll)
	defer ticker.Stop()
	for {
		if _, err := s.worker.ProcessOne(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/runs", s.submitRun)
	mux.HandleFunc("GET /v1/results/{locator}", s.getResult)
	return mux
}

func (s *Service) health(writer http.ResponseWriter, request *http.Request) {
	if err := s.ready(request.Context()); err != nil {
		slog.Warn("readiness check failed", "error", err)
		writeError(writer, http.StatusServiceUnavailable, "service is not ready")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

type submitRequest struct {
	Target    generation.CardTarget `json:"target"`
	MatchID   string                `json:"match_id"`
	CardState generation.CardState  `json:"card_state"`
	Inputs    map[string]any        `json:"inputs"`
	Mode      generation.RunMode    `json:"mode"`
}

type requestError struct{ error }

func (s *Service) submitRun(writer http.ResponseWriter, request *http.Request) {
	var submitted submitRequest
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&submitted); err != nil {
		slog.Debug("generation run rejected", "reason", "invalid_json")
		writeError(writer, http.StatusBadRequest, "invalid JSON request")
		return
	}
	role, err := s.roleResolver(request)
	if err != nil || !knownRole(role) {
		slog.Warn("generation run rejected", "reason", "unauthorized_caller")
		writeError(writer, http.StatusForbidden, "caller is not authorized")
		return
	}
	inputs, err := s.resolvedInputs(request.Context(), submitted)
	if err != nil {
		status := http.StatusBadRequest
		if !isRequestError(err) {
			status = http.StatusServiceUnavailable
		}
		writeError(writer, status, err.Error())
		return
	}
	run, err := s.module.StartRun(request.Context(), generation.StartRequest{Target: submitted.Target, MatchID: submitted.MatchID, CardState: submitted.CardState, Inputs: inputs, Caller: generation.Caller{Role: role}, Mode: submitted.Mode})
	if errors.Is(err, generation.ErrUnauthorized) {
		slog.Warn("generation run rejected", "reason", "unauthorized_regeneration", "template", submitted.Target.Template, "card_set", submitted.Target.CardSet, "match_id", submitted.MatchID)
		writeError(writer, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		slog.Warn("generation run rejected", "reason", "invalid_request", "template", submitted.Target.Template, "card_set", submitted.Target.CardSet, "match_id", submitted.MatchID, "error", err)
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	locators, err := s.module.ResultLocators(run)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("generation run accepted", "generation_run_id", run.ID, "parent_generation_run_id", run.ParentID, "template", submitted.Target.Template, "card_set", submitted.Target.CardSet, "match_id", submitted.MatchID, "card_state", submitted.CardState, "mode", run.Mode, "role", role)
	if submitted.Target.Template != "" {
		writeJSON(writer, http.StatusAccepted, map[string]string{"result_locator": locators[submitted.Target.Template]})
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]map[generation.TemplateID]string{"result_locators": locators})
}

func (s *Service) resolvedInputs(ctx context.Context, submitted submitRequest) (map[string]any, error) {
	for _, key := range []string{"team_a", "team_b", "format", "venue", "host_country", "target_start", "player_context"} {
		if _, ok := submitted.Inputs[key]; ok {
			return nil, invalidRequestf("%s is derived from match_id and must not be supplied", key)
		}
	}
	templates, ok := targetTemplates(s.config, submitted.Target)
	if !ok {
		return nil, generation.ErrInvalidRequest
	}
	context, err := s.matchContext.ResolveMatchContext(ctx, submitted.MatchID)
	if err != nil {
		return nil, err
	}
	inputs := make(map[string]any, len(submitted.Inputs)+5)
	for key, value := range submitted.Inputs {
		inputs[key] = value
	}
	needsVenue, needsPlayers := false, false
	for _, template := range templates {
		if _, ok := template.Allowed["team_a"]; ok {
			inputs["team_a"] = strconv.Itoa(context.TeamA)
		}
		if _, ok := template.Allowed["team_b"]; ok {
			inputs["team_b"] = strconv.Itoa(context.TeamB)
		}
		if _, ok := template.Allowed["format"]; ok {
			inputs["format"] = context.Format
		}
		if template.ID == generation.H2HRecord {
			inputs["target_start"] = context.Start.UTC().Format(time.RFC3339)
			if context.Venue > 0 {
				inputs["venue"] = context.Venue
			}
			if context.Country > 0 {
				inputs["host_country"] = context.Country
			}
		}
		needsVenue = needsVenue || template.ID == generation.VenueDNA || template.ID == generation.PlayerStatsAtVenue
		needsPlayers = needsPlayers || template.ID == generation.PlayerStatsAtVenue || template.ID == generation.Last5Games
	}
	if needsVenue {
		if context.Venue <= 0 {
			return nil, invalidRequestf("venue is unavailable for match_id %s", submitted.MatchID)
		}
		inputs["venue"] = strconv.Itoa(context.Venue)
	}
	if needsPlayers {
		players, details, err := selectedPlayers(submitted.MatchID, submitted.Inputs["players"], context.Players)
		if err != nil {
			return nil, err
		}
		inputs["players"], inputs["player_context"] = players, details
	}
	return inputs, nil
}

func targetTemplates(config generation.Configuration, target generation.CardTarget) ([]generation.Template, bool) {
	if target.Template != "" && target.CardSet != "" {
		return nil, false
	}
	if target.Template != "" {
		template, ok := config.Templates[target.Template]
		return []generation.Template{template}, ok
	}
	ids, ok := config.CardSets[target.CardSet]
	if !ok {
		return nil, false
	}
	templates := make([]generation.Template, 0, len(ids))
	for _, id := range ids {
		templates = append(templates, config.Templates[id])
	}
	return templates, true
}

func selectedPlayers(matchID string, supplied any, squad []generation.MatchPlayer) ([]string, map[string]any, error) {
	byID := make(map[string]generation.MatchPlayer, len(squad))
	for _, player := range squad {
		byID[strconv.Itoa(player.ID)] = player
	}
	if len(byID) == 0 {
		return nil, nil, invalidRequestf("match squad is unavailable for match_id %s", matchID)
	}
	requested, err := requestedPlayerIDs(supplied)
	if err != nil {
		return nil, nil, err
	}
	if len(requested) == 0 {
		for id := range byID {
			requested = append(requested, id)
		}
	}
	sort.Strings(requested)
	requested = uniqueStrings(requested)
	details := make(map[string]any, len(requested))
	for _, id := range requested {
		player, ok := byID[id]
		if !ok {
			return nil, nil, invalidRequestf("player %s is not in match squad for match_id %s", id, matchID)
		}
		details[id] = map[string]any{"team_id": player.TeamID, "player_name": player.FullName}
	}
	return requested, details, nil
}

func requestedPlayerIDs(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			values = make([]any, len(strings))
			for index := range strings {
				values[index] = strings[index]
			}
		} else {
			return nil, invalidRequestf("players must be a stable ID list")
		}
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || id <= 0 {
			return nil, invalidRequestf("players must be a stable ID list")
		}
		ids = append(ids, strconv.Itoa(id))
	}
	return ids, nil
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func isRequestError(err error) bool {
	var request requestError
	return errors.As(err, &request) || errors.Is(err, generation.ErrInvalidRequest) || errors.Is(err, generation.ErrMatchNotFound) || errors.Is(err, generation.ErrUnsupportedMatchType)
}

func invalidRequestf(format string, values ...any) error {
	return requestError{fmt.Errorf(format, values...)}
}

func (s *Service) getResult(writer http.ResponseWriter, request *http.Request) {
	result, err := s.module.GetResult(request.Context(), request.PathValue("locator"))
	if errors.Is(err, generation.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "result locator not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if result.Result != nil && result.Result.NoContent {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func knownRole(role generation.Role) bool {
	return role == generation.Scheduler || role == generation.Editor || role == generation.Operations
}

func localRole(request *http.Request) (generation.Role, error) {
	role := generation.Role(request.Header.Get("X-Insights-Role"))
	if role == "" {
		role = generation.Editor
	}
	return role, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

type unavailableData struct{}

type unavailableMatchContext struct{}

func (unavailableMatchContext) ResolveMatchContext(context.Context, string) (generation.MatchContext, error) {
	return generation.MatchContext{}, generation.ErrHistoricalUnavailable
}

func (unavailableData) Generate(_ context.Context, query generation.GenerationQuery) generation.GeneratedData {
	slog.Warn("historical source is not configured", "template", query.Template, "match_id", query.MatchID, "action", "set production mode and replica configuration")
	return generation.GeneratedData{Err: &generation.RunFailure{Kind: generation.ConfigurationFailure, Message: fmt.Sprintf("historical data source is not configured for %s", strings.ReplaceAll(string(query.Template), "_", " "))}}
}

type emptyEligibleMatches struct{}

func (emptyEligibleMatches) EligibleMatches(context.Context) ([]generation.EligibleMatch, error) {
	return nil, nil
}
