# Shared Match phase rules

## Status

Accepted

## Decision

One Match phase module owns the ordered Powerplay, Middle, and Death ranges for supported T20 and ODI formats. Team Phase Profiles and Venue DNA consume those named ranges while retaining their own historical-query implementation. Unsupported formats are rejected before generation.

## Consequences

One shared Match phase test table verifies all labels and ranges; each Card-template test verifies consumption. A change to Match phase meaning increases every affected Card template version so it creates a new result locator while prior Generation results remain auditable.
