# Insights automation

Run locally with SQLite:

```bash
go run ./cmd/insights-automation
```

The service listens on `:8080`, creates `data/insights-automation.db`, and exposes `GET /healthz`, `POST /v1/runs`, and `GET /v1/results/{result_locator}`.

The command loads a `.env` file from its working directory before reading configuration. Values already exported in your shell take precedence, and `.env` remains local-only.

Submit an approved normal request:

```bash
curl -X POST http://localhost:8080/v1/runs \
  -H 'Content-Type: application/json' \
  -d '{"target":{"template":"h2h_record"},"match_id":"m-1","card_state":"pre_toss","inputs":{"team_a":"1","team_b":"2","format":"t20"}}'
```

The submission returns a stable `result_locator`. Poll it instead of rebuilding filters in a URL or polling a run ID:

```bash
curl http://localhost:8080/v1/results/<result_locator>
```

The response always includes `result_status.status` (`queued`, `running`, `succeeded`, or `failed`). A succeeded response includes `result`; a failed response includes `failure`. A card-set submission returns `result_locators`, keyed by template, because each card has an independent result.

Local mode always stores generation runs and results in SQLite. To generate real cards locally, add the read-only replica settings `SITE_DB_USERNAME_NOMAD`, `SITE_DB_PASSWORD_NOMAD`, `SITE_DB_HOST`, and `SITE_DB` to `.env`; the service then reads historical data from that replica while continuing to write locally. Without those settings, local mode remains a durable API and queue smoke-test environment and submitted card runs record a visible configuration failure.

Set `INSIGHTS_AUTOMATION_LOG_LEVEL=debug` in `.env` to see run claims, request validation, worker execution, retries, and persistence events. Logs include run, template, match, and failure identifiers but never database credentials or request payloads.

To use a different local database file, set `INSIGHTS_AUTOMATION_SQLITE_PATH`. Production mode requires `INSIGHTS_AUTOMATION_MODE=production`, `INSIGHTS_AUTOMATION_WRITE_DSN`, and the existing replica variables `SITE_DB_USERNAME_NOMAD`, `SITE_DB_PASSWORD_NOMAD`, `SITE_DB_HOST`, and `SITE_DB`.
