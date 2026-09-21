# Durable generation ledger

## Status

Accepted

## Decision

One durable ledger module owns idempotent submission, cross-process leases, retry timing and attempts, immutable Generation result versions, Current-result pointers, and terminal Card-set run status. Workers keep only process-local query slots, execution deadlines, and outcome classification. SQLite and MariaDB are adapters behind the durable ledger seam, and each executes the same lifecycle test suite.

## Consequences

Publication of a Generation result, its Current-result pointer, the child Generation-run status, and any newly terminal Card-set run status is one atomic durable transition. Result locators, Normal request coalescing, Regeneration priority, and the storage identities in ADR-0006 remain unchanged; no compatibility migration or table redesign is required.
