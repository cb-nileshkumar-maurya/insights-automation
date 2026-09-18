# Explicit generation storage identities

## Status

Accepted

## Decision

Generation storage uses explicit identity names: `generation_run_id`, `parent_generation_run_id`, `generation_result_id`, `generation_run_id` on generated results, and `result_locator` for the stable opaque lookup identity. The table that maps a locator to its newest successful result is `generation_result_pointers`. Retain the repeated locator, template, version, match, and card-state fields in the run, immutable-result, and pointer records because they provide direct lookup and auditable provenance.

This is a clean-schema change. Update the SQLite bootstrap schema, canonical schema, and all code references without a compatibility migration; existing local SQLite files must be removed before use. The write-schema definition is updated for future MariaDB provisioning, but no live-database migration is supplied.
