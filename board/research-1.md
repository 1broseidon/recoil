---
id: research-1
title: MemPalace comparison takeaways
type: research
column: reference
position: 4
priority: medium
tags:
  - research
  - mempalace
  - v0
parentId: epic-1
createdAt: "2026-05-11T02:25:43.326Z"
---

## Description
MemPalace is stronger as a full memory app today because it has mining, sweep, wake-up, MCP, hooks, repair, and benchmark artifacts. Recoil should stay the fast Go shell primitive and close gaps around mine, eval, layered wake, and staleness.

## Findings

MemPalace 3.3.4 is more mature as a complete memory application:

- `mine`, `split`, and `sweep` already ingest project and conversation data.
- `wake-up` is a recognizable product concept.
- hooks and MCP are already present.
- benchmark artifacts and LongMemEval-style claims create credibility.

Local trial takeaway:

- MemPalace had better import/completeness.
- Recoil had much faster shell ergonomics and simpler sourced output.

## Recoil Lane

Recoil should not copy the palace metaphor or broad surface. It should win as:

- a single Go binary,
- deterministic direct `add`,
- implied project scope,
- fast local SQLite FTS5 retrieval,
- agent-readable output,
- explicit lifecycle/trust controls.

## Resulting Tasks

- `task-1`: eval fixtures.
- `task-2`: eval harness.
- `task-3`: first miner.
- `task-4`: layered wake.
- `task-5` through `task-7`: stale-memory model and retrieval.
