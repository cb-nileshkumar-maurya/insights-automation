// Package app adapts the generation module into a runnable HTTP service.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cricbuzz/insights-automation/generation"
)

type Config struct {
	WriteDatabase generation.DatabaseConfig
	Data          generation.CricketData
	Eligible      generation.EligibleMatchSource
	RoleResolver  RoleResolver
	WorkerPoll    time.Duration
	WorkerID      string
	Ready         func(context.Context) error
}

type Service struct {
	store        *generation.SQLRunStore
	module       *generation.SQLModule
	worker       *generation.SQLWorker
	reconciler   *generation.Reconciler
	workerPoll   time.Duration
	roleResolver RoleResolver
	ready        func(context.Context) error
}

// RoleResolver is the command's authentication boundary. Production callers
// receive a role from an authenticated adapter, never from request JSON.
type RoleResolver func(*http.Request) (generation.Role, error)

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
	module := generation.NewSQLModule(generation.DefaultConfiguration(), store, generation.ClockFunc(func() time.Time { return time.Now().UTC() }))
	if config.Eligible == nil {
		// Match discovery is injected because the authoritative upcoming-match
		// and squad population source is deployment-specific.
		config.Eligible = emptyEligibleMatches{}
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
	return &Service{store: store, module: module, worker: generation.NewSQLWorker(store, config.Data, generation.DefaultConfiguration(), config.WorkerID), reconciler: generation.NewReconciler(config.Eligible, module, "pre_toss_v1"), workerPoll: config.WorkerPoll, roleResolver: config.RoleResolver, ready: config.Ready}, nil
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
	run, err := s.module.StartRun(request.Context(), generation.StartRequest{Target: submitted.Target, MatchID: submitted.MatchID, CardState: submitted.CardState, Inputs: submitted.Inputs, Caller: generation.Caller{Role: role}, Mode: submitted.Mode})
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
	slog.Info("generation run accepted", "run_id", run.ID, "parent_run_id", run.ParentID, "template", submitted.Target.Template, "card_set", submitted.Target.CardSet, "match_id", submitted.MatchID, "card_state", submitted.CardState, "mode", run.Mode, "role", role)
	if submitted.Target.Template != "" {
		writeJSON(writer, http.StatusAccepted, map[string]string{"result_locator": locators[submitted.Target.Template]})
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]map[generation.TemplateID]string{"result_locators": locators})
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

func (unavailableData) Generate(_ context.Context, query generation.GenerationQuery) generation.GeneratedData {
	slog.Warn("historical source is not configured", "template", query.Template, "match_id", query.MatchID, "action", "set production mode and replica configuration")
	return generation.GeneratedData{Err: &generation.RunFailure{Kind: generation.ConfigurationFailure, Message: fmt.Sprintf("historical data source is not configured for %s", strings.ReplaceAll(string(query.Template), "_", " "))}}
}

type emptyEligibleMatches struct{}

func (emptyEligibleMatches) EligibleMatches(context.Context) ([]generation.EligibleMatch, error) {
	return nil, nil
}
