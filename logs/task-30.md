---
id: task-30
title: "Executable external predicate: run a command, compare its output hash"
priority: medium
tags:
  - decisions
  - predicates
  - staleness
  - security
relatedFiles:
  - cmd/decision_predicate.go
  - cmd/decide.go
parentId: epic-3
createdAt: "2026-09-23T02:59:35.349Z"
completedAt: "2026-09-23T20:53:58.830Z"
updatedAt: "2026-09-23T20:53:58.830Z"
---

## Description
The external tier always returns needs_review (cmd/decision_predicate.go:256-258). Give it a deterministic form: `recoil decide --predicate-cmd "<command>"` runs the command in the project root at decide time, stores the sha256 of stdout, and `check` re-runs it and reports holds or violated. Any fact expressible as command output can then govern a decision: a function body, a generated schema, a config key, an API response. Recoil stays ignorant of which tool produced the output (for example a code indexer's per-symbol hash).

Security is the design constraint: memories can be written by agents, so a stored command is untrusted input. Never run predicate commands from hooks by default; show the command and require an explicit `check --run-predicates` or a project-level allowlist in settings; enforce a timeout and no shell unless configured.

Done when: a decision anchored to `<cmd>` flips to violated when that output changes, and nothing executes without the operator's opt-in.

## Log
- 2026-09-23T20:53:58.022Z: [claude] Closed, won't do (2026-09-23). Executable predicates would have recoil run commands stored by agents, which is a security surface with opt-in gates to design and maintain. source_unchanged hashes and valid_until already cover staleness deterministically.
