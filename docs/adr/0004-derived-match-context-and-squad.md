# Derived match context and squad

## Status

Accepted

## Decision

`POST /v1/runs` accepts `match_id` as the authority for teams, match format, and venue. Clients must not submit `team_a`, `team_b`, `format`, or `venue`; such fields are rejected as invalid inputs. The service reads the match from `krik_match_archive`, maps its supported `match_type_id` to the internal format, and obtains venue metadata from `krik_match_venue` where a template requires it. Head-to-head remains format-wide and does not use the target match venue.

Inputs are optional. The only client-selected filters are template-approved selectors such as `latest_matches`, `window`, and an optional `players` override. The resolved match context is inserted into each template's normalized inputs before its result locator is computed. A correction to the match record or squad can therefore produce a different locator.

For player templates, the default player population is every row in `stats_import3_dump_matchplayers_tbl` for the submitted `match_id`, including substitutes and super-subs. An omitted or empty `players` input uses that population. A non-empty override must be a subset of that population; it is deduplicated and sorted before locator creation. Player result entries use the squad row's `teamId`, and resolve `player_name` from `stats_import_player_master.fullName`.

A missing match, unsupported match format, required missing venue, unavailable squad, or player override outside the match squad is a `400` request failure with a specific message. A failure to read the historical source is a `503` service failure.

## Consequences

Callers send less duplicated match context and cannot manufacture a card for teams or a venue that do not belong to the match. Submission now depends on the read replica being available, and a card's locator represents the resolved match and squad facts as well as any allowed client selectors.
