---
id: task-15
title: Role-aware aging for handoffs/notes + inline valid_until evaluation in wake/search
priority: high
tags:
  - audit
  - stale-memory
  - lifecycle
relatedFiles:
  - cmd/wake.go
  - cmd/search.go
  - cmd/decision_predicate.go
  - cmd/repair.go
parentId: epic-1
createdAt: "2026-06-10T04:40:31.770Z"
completedAt: "2026-06-10T05:03:53.808Z"
updatedAt: "2026-06-10T05:03:53.808Z"
---

## Description
Nothing in the codebase ages memories by time — validity only changes via explicit mark/supersede/re-mine, so a January handoff is still active in July and surfaces in wake as current guidance. Fix (read-side first, non-destructive): (1) handoff older than the newest handoff in scope renders as historical automatically; (2) handoff/note older than a configurable window (30/90d) gets a ranking penalty plus why: aging annotation; (3) decision/constraint/preference/adr never age by time alone. Also evaluate cheap deterministic predicates inline during wake/search — an expired valid_until (cmd/decision_predicate.go:260-273) currently only fires in check, so an expired decision still renders as clean current guidance. Add recoil repair --age as the explicit write-side demotion.
