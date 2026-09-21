# Canonical Player population

## Status

Accepted

## Decision

The Player population module owns stable-ID ordering, squad membership validation, and Player identity. Intake constructs that value from Derived match context and an optional approved override; Player Stats At Venue and Last 5 Games consume it directly. Its canonical encoding supplies result-locator inputs and immutable Generation-result provenance.

## Consequences

A squad row without a canonical `fullName` in the player master rejects the submission with its stable ID; it is never silently omitted. A corrected squad team ID or Player identity creates a new result locator while prior Generation results remain auditable. This supersedes ADR-0007's temporary retention of the paired `players` and `player_context` representation.
