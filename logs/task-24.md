---
id: task-24
title: "Wake layer quotas: handoff and decisions must not be crowded out by doc chunks"
priority: high
tags:
  - audit
  - wake
  - bug
  - continuity
relatedFiles:
  - cmd/wake.go
  - cmd/sourcequality_wake_refs.go
parentId: epic-1
createdAt: "2026-06-10T05:11:09.407Z"
completedAt: "2026-06-10T05:27:09.717Z"
updatedAt: "2026-06-10T05:27:09.717Z"
---

## Description
Found in live smoke test: with default limit 8, wake's entire selected set was README chunks (project_docs lane); the freshest handoff and current decisions never surfaced, defeating the wake→handoff continuity loop. Two causes in cmd/wake.go: (1) wakeRecentMemories quota-lists SourcePath HANDOFF.md (a file) and roles adr/decision/constraint/preference/rule but never role=handoff, so direct handoff memories only enter via the generic tail fetch; (2) rankWakeCandidates sorts all candidates by sourcequality score — README chunks earn ~1.85 from the wakePolicyQuery onboarding boost while handoffs/decisions earn 0, so docs fill all slots before layer classification happens. Fix: (a) add role=handoff to the quota list in wakeRecentMemories (newest only, consistent with the task-15 newest-handoff rule); (b) enforce per-layer slot quotas in buildWakeLayers (e.g. current_decisions >=2, recent_evidence >=2 incl. newest handoff, project_docs capped at ~half of limit) instead of pure global score order; (c) bound per-memory render length or give per-layer char budgets in layeredMemoryBlocks so one fat doc chunk cannot eat the whole --max-chars budget (smoke test: 1600 chars rendered only 1 of 8 selected). Must keep wake-safety eval guarantees: still exclude historical/expired; eval + workflows suites must pass.
