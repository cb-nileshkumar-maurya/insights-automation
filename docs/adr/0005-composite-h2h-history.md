# Composite H2H history

## Status

Accepted

## Decision

The next version of `h2h_record` remains one match-scoped template and one result locator, but its result combines `head_to_head` and `team_form`. H2H has independently windowed, exact-format `overall` and `at_venue` slices; team form has independently windowed, exact-format `overall`, `at_venue`, and `in_host_country` slices. H2H defaults to 10 records and retains `latest_matches`; team form defaults to 5 records and uses `team_form_latest_matches` when overridden. Every historical record is strictly before the target match start date and includes `batting_first_team_id` and `chasing_team_id` when historical innings establish them.

The target venue ID and its country come from authoritative match/venue data. If either is missing, omit only the dependent slice and log the reason; do not suppress other slices. A valid scoped population with no records is returned as an explicit empty slice. A completed locator returns HTTP 204 only when no H2H overall records and no overall form records for either team exist; if either section has overall history, return the composite result with the unavailable section explicit and empty. Existing locators retain the old versioned payload.

## Consequences

This supersedes ADR 0002 and the H2H venue-exclusion portion of ADR 0004. It replaces the independent `team_form` locator in the H2H experience with a composite, versioned H2H response; direct `team_form` behavior is not changed by this decision.
