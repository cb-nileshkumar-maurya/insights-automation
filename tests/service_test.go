package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/cricbuzz/insights-automation/app"
	"github.com/cricbuzz/insights-automation/generation"
)

func TestLocalServiceStartsAndServesGeneratedCards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := app.New(ctx, app.Config{
		WriteDatabase: generation.DatabaseConfig{Dialect: generation.SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")},
		Data:          &generation.ScriptedCricketData{Responses: map[generation.TemplateID]generation.GeneratedData{generation.H2HRecord: {Data: map[string]any{"wins": 3}, SampleSize: 3, SourceDataWindow: "2024-01-01..2026-01-01"}}},
		WorkerPoll:    time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	go service.Run(ctx)

	server := httptest.NewServer(service.Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/healthz")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("health response=%v err=%v", response, err)
	}
	response.Body.Close()

	body := []byte(`{"target":{"template":"h2h_record"},"match_id":"m-1","card_state":"pre_toss","inputs":{"team_a":"1","team_b":"2","format":"t20"},"role":"editor"}`)
	response, err = http.Post(server.URL+"/v1/runs", "application/json", bytes.NewReader(body))
	if err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status=%v err=%v", response, err)
	}
	response.Body.Close()

	filters, _ := json.Marshal(map[string]any{"team_a": "1", "team_b": "2", "format": "t20"})
	resultURL := server.URL + "/v1/results?template=h2h_record&match_id=m-1&card_state=pre_toss&inputs=" + url.QueryEscape(string(filters))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(resultURL)
		if err == nil && response.StatusCode == http.StatusOK {
			var result generation.Result
			err = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err == nil && result.Data["wins"] == float64(3) {
				return
			}
		} else if response != nil {
			response.Body.Close()
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("current generated card was not available")
}

func TestLocalServiceMakesMissingHistoricalSourceVisible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := app.New(ctx, app.Config{WriteDatabase: generation.DatabaseConfig{Dialect: generation.SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")}, WorkerPoll: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	go service.Run(ctx)
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/runs", "application/json", bytes.NewBufferString(`{"target":{"template":"h2h_record"},"match_id":"m-1","card_state":"pre_toss","inputs":{"team_a":"1","team_b":"2","format":"t20"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var submitted generation.Run
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(server.URL + "/v1/runs/" + submitted.ID)
		if err == nil && response.StatusCode == http.StatusOK {
			var run generation.Run
			err = json.NewDecoder(response.Body).Decode(&run)
			response.Body.Close()
			if err == nil && run.State == generation.Failed {
				if run.Failure == nil || run.Failure.Kind != generation.ConfigurationFailure || run.Failure.Message != "historical data source is not configured for h2h record" {
					t.Fatalf("failure=%#v", run.Failure)
				}
				return
			}
		} else if response != nil {
			response.Body.Close()
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("missing historical source was not reported")
}
