---
id: decision-1
title: "Freshness model: supersession, not deletion"
type: decision
column: reference
position: 0
priority: high
tags:
  - product
  - staleness
  - v0.1
parentId: epic-1
createdAt: "2026-05-11T02:25:43.315Z"
---

## Description
Stale memory should be handled by validity states, claim keys, and supersession links. Preserve rejected/superseded history, but make current guidance unmistakable in wake and search.

## Decision

Do not solve stale memory by deleting history. Old attempts and failures often
matter. Solve it by making validity visible.

## Validity Model

- `active`: current guidance, decision, constraint, or preference.
- `historical`: useful evidence, not current guidance by itself.
- `rejected`: tried and intentionally not used.
- `superseded`: replaced by a newer memory.
- `stale`: suspected obsolete, needs review.
- `unknown`: imported or mined memory with no validity decision.
- `tombstoned`: removed from recall.

## Retrieval Contract

- `wake` orients around active decisions, constraints, and handoffs.
- `wake` excludes superseded memories by default.
- `search` shows current results first.
- `search` can show rejected/superseded history in a labeled section.

## Example

If an old memory says "we tried swlote" and a newer memory says "we use
github.com/mattn/go-sqlite3 for FTS5", search should surface both, but only the
mattn decision may appear as current guidance.

## Follow-Up Tasks

- `task-1`: include stale/superseded cases in eval fixtures.
- `task-5`: add validity metadata and supersession links.
- `task-6`: add `mark` and `supersede`.
- `task-7`: make `search` and `wake` stale-aware.
