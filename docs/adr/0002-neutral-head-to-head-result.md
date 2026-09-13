# Neutral head-to-head result

## Status

Accepted

## Decision

The H2H Record card returns a neutral `summary` with symmetric team aggregates and a newest-first `head_to_head` list of historical match records. Each record references teams by ID, uses `start_date`, retains structured outcome facts plus `result_string`, and distinguishes draws from no-results; team names appear once in the summary. This replaces team-A-oriented wins, losses, win rate, and recent sequence, because an H2H card is a match-scoped result rather than either team's history.

## Consequences

The approved requested match counts are 5, 10, 15, and 20, with a default of 10. A shorter non-zero history remains successful, reports its actual sample size, and includes the `available_history` fallback. Venue, series, and per-match innings data are excluded from this response contract.
