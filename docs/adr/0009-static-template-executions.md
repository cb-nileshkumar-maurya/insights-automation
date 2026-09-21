# Static template executions

## Status

Accepted

## Decision

Each approved Card template is a template-execution module that owns its version, freshness policy, allowed inputs, generation rules, source-data window, and result semantics. The deployment statically assembles the approved set at startup; the worker executes a selected template without dispatch or input-policy knowledge. Card sets remain small declarations that compose approved template executions, while each template execution owns its own Card-state eligibility.

## Consequences

Unknown templates are rejected before durable submission. Specialized historical-data seams remain internal to the relevant template-execution module; no generic historical-data adapter or runtime registration mechanism is introduced. Template tests use their execution interface, and worker tests use a scripted execution adapter for worker behavior only.
