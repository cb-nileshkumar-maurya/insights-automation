package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var submitted struct {
		ResultLocator string `json:"result_locator"`
	}
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil || submitted.ResultLocator == "" {
		t.Fatalf("submitted=%#v err=%v", submitted, err)
	}
	response.Body.Close()

	resultURL := server.URL + "/v1/results/" + submitted.ResultLocator
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(resultURL)
		if err == nil && response.StatusCode == http.StatusOK {
			var result struct {
				ResultStatus struct {
					Status string `json:"status"`
				} `json:"result_status"`
				Result *generation.Result `json:"result"`
			}
			err = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err == nil && result.ResultStatus.Status == "succeeded" && result.Result != nil && result.Result.Data["wins"] == float64(3) {
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
	var submitted struct {
		ResultLocator string `json:"result_locator"`
	}
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.ResultLocator == "" {
		t.Fatal("missing result locator")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(server.URL + "/v1/results/" + submitted.ResultLocator)
		if err == nil && response.StatusCode == http.StatusOK {
			var result struct {
				ResultStatus struct {
					Status string `json:"status"`
				} `json:"result_status"`
				Failure *generation.RunFailure `json:"failure"`
			}
			err = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if err == nil && result.ResultStatus.Status == "failed" {
				if result.Failure == nil || result.Failure.Kind != generation.ConfigurationFailure || result.Failure.Message != "historical data source is not configured for h2h record" {
					t.Fatalf("failure=%#v", result.Failure)
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

func TestServiceReturnsOneLocatorPerCardSetTemplate(t *testing.T) {
	ctx := context.Background()
	service, err := app.New(ctx, app.Config{WriteDatabase: generation.DatabaseConfig{Dialect: generation.SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	response, err := http.Post(server.URL+"/v1/runs", "application/json", bytes.NewBufferString(`{"target":{"card_set":"pre_toss_v1"},"match_id":"m-1","card_state":"pre_toss","inputs":{"team_a":"1","team_b":"2","players":["3"],"venue":"4","format":"t20"}}`))
	if err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status=%v err=%v", response, err)
	}
	defer response.Body.Close()
	var submitted struct {
		ResultLocators map[string]string `json:"result_locators"`
	}
	if err := json.NewDecoder(response.Body).Decode(&submitted); err != nil {
		t.Fatal(err)
	}
	if len(submitted.ResultLocators) != 6 || submitted.ResultLocators[string(generation.H2HRecord)] == "" {
		t.Fatalf("locators=%#v", submitted.ResultLocators)
	}
}

func TestServiceReturnsNotFoundForUnknownResultLocator(t *testing.T) {
	service, err := app.New(context.Background(), app.Config{WriteDatabase: generation.DatabaseConfig{Dialect: generation.SQLite, DSN: filepath.Join(t.TempDir(), "generation.db")}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(service.Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/v1/results/missing")
	if err != nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("response=%v err=%v", response, err)
	}
	response.Body.Close()
}
