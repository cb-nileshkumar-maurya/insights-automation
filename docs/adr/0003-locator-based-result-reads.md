# Locator-based result reads

## Status

Accepted

## Decision

Template-run submissions return the existing stable input hash as an opaque result locator: a 64-character SHA-256 value over the template version, match, card state, and normalized filters. Clients read `GET /v1/results/{locator}`, which returns `result_status.status` and includes the current generation result only when the state is succeeded; failed states include failure details, and unknown locators return 404. This replaces client-built template, match, card-state, and JSON-filter query strings, and removes the need for normal callers to poll a run ID. The locator is an identifier, not an authorization token.

Card-set submissions return one child result locator per card template because a card-set parent has no single generation result. A locator with an existing successful result remains succeeded and returns that result while a later regeneration is queued or running; only a successfully completed regeneration replaces the current result.
