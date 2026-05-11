---
id: epic-1
title: v0 Local-Only Recoil Core
type: epic
column: roadmap
position: 0
description: "Build Recoil as a fast local-first memory CLI: deterministic append-only writes, SQLite FTS5 recall, explicit scopes, agent-readable output, and no network dependency. This is the Cymbal-for-memory foundation and should stay boring, sharp, and inspectable."
priority: high
tags:
  - roadmap
  - v0
  - local-first
  - cli
  - memory
subtasks:
  - id: epic-1-1
    title: "Migrated history: finish add/search/show/status polish"
    completed: true
  - id: epic-1-2
    title: "Migrated history: add list and forget commands"
    completed: true
  - id: epic-1-3
    title: "Migrated history: implement wake as bounded starter context"
    completed: true
  - id: epic-1-4
    title: "Migrated to task-8: add project/user/session scope tests"
    completed: true
  - id: epic-1-5
    title: "Migrated to task-3: add first conservative transcript miner"
    completed: true
  - id: epic-1-6
    title: "Migrated history: write Codex/Claude/OpenCode instruction snippets"
    completed: true
  - id: epic-1-7
    title: "Migrated history: review and finalize v0 CLI surface spec"
    completed: true
  - id: epic-1-8
    title: "Migrated to task-1: create v0 memory retrieval golden eval set"
    completed: true
  - id: epic-1-9
    title: "Migrated to task-2: implement v0 eval harness"
    completed: true
  - id: epic-1-10
    title: "Migrated history: define wake ranking and budgeting algorithm"
    completed: true
  - id: epic-1-11
    title: "Migrated history: add init/config/instructions commands"
    completed: true
  - id: epic-1-12
    title: "Migrated history: document v0 non-goals in README"
    completed: true
  - id: epic-1-13
    title: "Migrated to research: competitive positioning vs other local memory tools"
    completed: true
  - id: epic-1-14
    title: "Migrated to task-9: add local performance benchmark"
    completed: true
  - id: epic-1-15
    title: "Migrated to task-4: improve wake into layered current context"
    completed: true
  - id: epic-1-16
    title: "Migrated to decision-2: define problem and usability story"
    completed: true
  - id: epic-1-17
    title: "Migrated to decision-1: design stale memory validity model"
    completed: true
  - id: epic-1-18
    title: "Migrated to task-1: add stale/superseded eval cases"
    completed: true
  - id: epic-1-19
    title: "Migrated to task-5: add validity status and supersession links"
    completed: true
  - id: epic-1-20
    title: "Migrated to task-6: add mark and supersede lifecycle commands"
    completed: true
createdAt: "2026-05-11T01:00:34.261Z"
contract:
  status: ready
  deliverables:
    - type: file
      path: cmd/add.go
      description: Add verbatim memory ingestion with smart text/file/stdin input.
    - type: file
      path: cmd/search.go
      description: Fast scoped memory search with frontmatter, JSON, and minimal outputs.
    - type: file
      path: cmd/show.go
      description: Stable memory lookup by ID or unique prefix.
    - type: file
      path: cmd/status.go
      description: Local database health/status command.
    - type: file
      path: internal/store
      description: SQLite schema, FTS5 migrations, deterministic IDs, redaction, dedupe, and tests.
    - type: docs
      path: docs/product.md
      description: Compact product index pointing to Brainfile decisions, research, and active child tasks.
  validation:
    commands:
      - make test
      - make build
  constraints:
    - No cloud, daemon, hosted service, or network dependency in the core path.
    - No LLM in the default write path; persistence must remain deterministic.
    - SQLite FTS5 is required for search; no degraded fallback behavior.
    - Default output must remain useful for agents without requiring JSON parsing.
    - "Scope discipline must be explicit: user, project, and session memories cannot blur together."
    - "Competitive takeaway: mine/eval/wake quality are core v0 usefulness work, while Recoil should keep its simpler CLI vocabulary."
    - "Problem framing: make local evidence cheaper to retrieve than guessing."
    - "Stale-memory framing: preserve rejected/superseded history while making current guidance unmistakable."
  metrics:
    readyAt: "2026-05-11T01:00:34.261Z"
    updatedAt: "2026-05-11T03:15:00.000Z"
---

## Description
Build Recoil as a fast local-first memory CLI: deterministic append-only writes, SQLite FTS5 recall, explicit scopes, agent-readable output, and no network dependency. This is the Cymbal-for-memory foundation and should stay boring, sharp, and inspectable.

Inline subtasks above are retained as migrated history. Active implementation
work now lives as child board files with `parentId: epic-1`; settled product
decisions and research live in the `reference` column.
