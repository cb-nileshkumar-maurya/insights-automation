# Generation module and versioned results

## Status

Accepted

## Decision

The generation module is the only public boundary for creating and reading generated cards. It exposes `StartRun`, `GetRun`, and `GetCurrentResult`.

`StartRun` accepts a named card set or an approved card template, stable IDs, and plain values. It validates and normalizes those values using repository-owned template configuration. Callers cannot provide SQL, arbitrary date ranges, or arbitrary match counts.

Successful runs write immutable generation results. A current-result pointer identifies the latest successful, non-stale version for each normalized template input. A failed regeneration never replaces that pointer.

## Consequences

The scheduler and selector do not need query knowledge. Storage and worker implementations can change without changing the external contract. Results retain their provenance and remain auditable after they become stale.
