# Insights automation

Run locally with SQLite:

```bash
go run ./cmd/insights-automation
```

The service listens on `:8080`, creates `data/insights-automation.db`, and exposes `GET /healthz`, `POST /v1/runs`, `GET /v1/runs/{id}`, and `GET /v1/results`.

Local mode deliberately has no historical cricket source. A submitted card run records a visible configuration failure until a production MariaDB replica is configured.

To use a different local database file, set `INSIGHTS_AUTOMATION_SQLITE_PATH`. Production mode requires `INSIGHTS_AUTOMATION_MODE=production`, `INSIGHTS_AUTOMATION_WRITE_DSN`, and the existing replica variables `SITE_DB_USERNAME_NOMAD`, `SITE_DB_PASSWORD_NOMAD`, `SITE_DB_HOST`, and `SITE_DB`.
