# Universal derived match context

## Status

Accepted

## Decision

Every generation submission, including HTTP and scheduler submissions, resolves authoritative match facts, Player population, and Player identity through one Derived match context module before entering the generation module. The scheduler decides only which match is eligible and its Card state; it cannot supply match facts or arbitrary inputs. The intake module and generation module share one Configuration instance, while the current normalized `players` and `player_context` representation remains unchanged.

## Consequences

Missing matches, unsupported formats, required missing venues, and invalid Player overrides are non-retriable rejections for every submission source. Historical-source outages remain operational failures. Tests cross the intake seam with a MatchContextResolver adapter; HTTP tests remain limited to authentication and response mapping.
