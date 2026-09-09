// Package app adapts the generation module into a runnable HTTP service.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cricbuzz/insights-automation/generation"
)

type Config struct {
	WriteDatabase generation.DatabaseConfig
	Data          generation.CricketData
	Eligible      generation.EligibleMatchSource
	WorkerPoll    time.Duration
	WorkerID      string
}

type Service struct {
	store      *generation.SQLRunStore
	module     *generation.SQLModule
	worker     *generation.SQLWorker
	reconciler *generation.Reconciler
	workerPoll time.Duration
}

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
	if config.Data == nil {
		config.Data = unavailableData{}
	}
	if config.WorkerPoll <= 0 {
		config.WorkerPoll = 100 * time.Millisecond
	}
	if config.WorkerID == "" {
		config.WorkerID = "insights-automation"
	}
	module := generation.NewSQLModule(generation.DefaultConfiguration(), store, generation.ClockFunc(func() time.Time { return time.Now().UTC() }))
	if config.Eligible == nil {
		config.Eligible = emptyEligibleMatches{}
	}
	return &Service{store: store, module: module, worker: generation.NewSQLWorker(store, config.Data, generation.DefaultConfiguration(), config.WorkerID), reconciler: generation.NewReconciler(config.Eligible, module, "pre_toss_v1"), workerPoll: config.WorkerPoll}, nil
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
	mux.HandleFunc("GET /v1/runs/{id}", s.getRun)
	mux.HandleFunc("GET /v1/results", s.getCurrentResult)
	return mux
}

func (s *Service) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

type submitRequest struct {
	Target    generation.CardTarget `json:"target"`
	MatchID   string                `json:"match_id"`
	CardState generation.CardState  `json:"card_state"`
	Inputs    map[string]any        `json:"inputs"`
	Role      generation.Role       `json:"role"`
	Mode      generation.RunMode    `json:"mode"`
}

func (s *Service) submitRun(writer http.ResponseWriter, request *http.Request) {
	var submitted submitRequest
	if err := json.NewDecoder(request.Body).Decode(&submitted); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON request")
		return
	}
	if !knownRole(submitted.Role) {
		writeError(writer, http.StatusForbidden, "unknown caller role")
		return
	}
	run, err := s.module.StartRun(request.Context(), generation.StartRequest{Target: submitted.Target, MatchID: submitted.MatchID, CardState: submitted.CardState, Inputs: submitted.Inputs, Caller: generation.Caller{Role: submitted.Role}, Mode: submitted.Mode})
	if errors.Is(err, generation.ErrUnauthorized) {
		writeError(writer, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, run)
}

func (s *Service) getRun(writer http.ResponseWriter, request *http.Request) {
	run, err := s.module.GetRun(request.Context(), request.PathValue("id"))
	if errors.Is(err, generation.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "generation run not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, run)
}

func (s *Service) getCurrentResult(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	var inputs map[string]any
	if err := json.Unmarshal([]byte(query.Get("inputs")), &inputs); err != nil {
		writeError(writer, http.StatusBadRequest, "inputs must be a JSON object")
		return
	}
	result, err := s.module.GetCurrentResult(request.Context(), generation.TemplateID(query.Get("template")), query.Get("match_id"), generation.CardState(query.Get("card_state")), inputs)
	if errors.Is(err, generation.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "current generated card not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func knownRole(role generation.Role) bool {
	return role == generation.Scheduler || role == generation.Editor || role == generation.Operations
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
	return generation.GeneratedData{Err: &generation.RunFailure{Kind: generation.ConfigurationFailure, Message: fmt.Sprintf("historical data source is not configured for %s", strings.ReplaceAll(string(query.Template), "_", " "))}}
}

type emptyEligibleMatches struct{}

func (emptyEligibleMatches) EligibleMatches(context.Context) ([]generation.EligibleMatch, error) {
	return nil, nil
}
