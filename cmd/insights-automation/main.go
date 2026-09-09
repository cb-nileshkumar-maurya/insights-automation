package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cricbuzz/insights-automation/app"
	"github.com/cricbuzz/insights-automation/generation"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config, replica, err := runtimeConfig(ctx)
	if err != nil {
		slog.Error("configure insights automation", "error", err)
		os.Exit(1)
	}
	if replica != nil {
		defer replica.Close()
	}
	service, err := app.New(ctx, config)
	if err != nil {
		slog.Error("start insights automation", "error", err)
		os.Exit(1)
	}
	defer service.Close()
	server := &http.Server{Addr: envOr("INSIGHTS_AUTOMATION_ADDR", ":8080"), Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("generation worker stopped", "error", err)
			stop()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("insights automation ready", "address", server.Addr, "mode", mode())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve insights automation", "error", err)
		os.Exit(1)
	}
}

func runtimeConfig(ctx context.Context) (app.Config, interface{ Close() error }, error) {
	runtimeMode := mode()
	if runtimeMode != "local" && runtimeMode != "production" {
		return app.Config{}, nil, errors.New("INSIGHTS_AUTOMATION_MODE must be local or production")
	}
	if runtimeMode == "local" {
		path := envOr("INSIGHTS_AUTOMATION_SQLITE_PATH", filepath.Join("data", "insights-automation.db"))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return app.Config{}, nil, err
		}
		return app.Config{WriteDatabase: generation.DatabaseConfig{Dialect: generation.SQLite, DSN: path}}, nil, nil
	}
	writeDSN := os.Getenv("INSIGHTS_AUTOMATION_WRITE_DSN")
	if writeDSN == "" {
		return app.Config{}, nil, errors.New("INSIGHTS_AUTOMATION_WRITE_DSN must be set in production mode")
	}
	replica, err := generation.OpenReadReplica(ctx)
	if err != nil {
		return app.Config{}, nil, err
	}
	data := generation.NewTemplateData(map[generation.TemplateID]generation.CricketData{
		generation.H2HRecord:          generation.NewH2HGenerator(generation.NewMariaDBH2HHistory(replica)),
		generation.TeamForm:           generation.NewTeamFormGenerator(generation.NewMariaDBTeamFormHistory(replica)),
		generation.VenueDNA:           generation.NewVenueDNAGenerator(generation.NewMariaDBVenueHistory(replica)),
		generation.PlayerStatsAtVenue: generation.NewPlayerVenueGenerator(generation.NewMariaDBPlayerVenueHistory(replica)),
		generation.Last5Games:         generation.NewLast5GamesGenerator(generation.NewMariaDBLast5History(replica)),
		generation.TeamPhaseProfiles:  generation.NewTeamPhaseProfilesGenerator(generation.NewMariaDBTeamPhaseHistory(replica)),
	})
	resolver, err := productionRoleResolver()
	if err != nil {
		replica.Close()
		return app.Config{}, nil, err
	}
	return app.Config{WriteDatabase: generation.DatabaseConfig{Dialect: generation.MariaDB, DSN: writeDSN}, Data: data, RoleResolver: resolver, Ready: replica.PingContext}, replica, nil
}

func mode() string { return envOr("INSIGHTS_AUTOMATION_MODE", "local") }
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func productionRoleResolver() (app.RoleResolver, error) {
	tokens := map[string]generation.Role{}
	for _, configured := range []struct {
		name string
		role generation.Role
	}{{"INSIGHTS_AUTOMATION_SCHEDULER_TOKEN", generation.Scheduler}, {"INSIGHTS_AUTOMATION_EDITOR_TOKEN", generation.Editor}, {"INSIGHTS_AUTOMATION_OPERATIONS_TOKEN", generation.Operations}} {
		if token := os.Getenv(configured.name); token != "" {
			tokens[token] = configured.role
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("at least one INSIGHTS_AUTOMATION_*_TOKEN must be set in production mode")
	}
	return func(request *http.Request) (generation.Role, error) {
		token := request.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(token, prefix) {
			return "", errors.New("missing bearer token")
		}
		role, ok := tokens[strings.TrimPrefix(token, prefix)]
		if !ok {
			return "", errors.New("invalid bearer token")
		}
		return role, nil
	}, nil
}
