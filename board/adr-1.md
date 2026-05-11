---
id: adr-1
title: "ADR: local SQLite FTS5 with deterministic writes"
type: adr
column: reference
position: 5
priority: high
tags:
  - adr
  - architecture
  - v0
  - sqlite
parentId: epic-1
createdAt: "2026-05-11T02:26:36.801Z"
---

## Description
Recoil's core path is a single Go binary using CGO SQLite FTS5. Writes persist redacted verbatim evidence deterministically. No cloud, daemon, embeddings, or LLM call is required for add/search/wake.

## Context

The project is inspired by Cymbal's success: one fast local binary, no service
ritual, and shell-native output that agents can use directly.

## Decision

Use Go plus `github.com/mattn/go-sqlite3` with FTS5 enabled through CGO.

The supported path is:

- local SQLite database,
- FTS5 required,
- deterministic IDs and dedupe,
- redaction before persistence and hashing,
- frontmatter output by default,
- JSON as an escape hatch.

## Consequences

- `make test` and `make build` set `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5`.
- There is no degraded search fallback if FTS5 is unavailable.
- Embeddings can be added later as derived records, not as the required path.
- The write path must stay boring: no model call, no cloud call, no daemon.
