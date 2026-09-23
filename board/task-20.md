---
id: task-20
title: Track last_retrieved_at + add recoil review queue for memory hygiene
column: todo
position: 9
priority: medium
tags:
  - audit
  - stale-memory
  - operator
relatedFiles:
  - internal/store/store.go
  - cmd/repair.go
parentId: epic-1
createdAt: "2026-06-10T04:41:02.213Z"
---

## Description
No retrieval tracking exists. Add last_retrieved_at/retrieval_count columns (single UPDATE after search/wake hits; WAL makes it cheap). Enables: recoil review — operator queue of active memories never retrieved in N days, claim families with multiple actives, and broken deterministic predicates. This is the Review lane the Authorship Principle implies but doesn't have. Future: mild micro-prior for repeatedly-retrieved never-superseded memories.

## Log
- 2026-09-23T20:53:57.743Z: [claude] Closed, won't do (2026-09-23). Tracking last_retrieved_at turns every search and wake into a write, which undoes the read-only open that search and check just got back. A review queue is an operator dashboard, not a stateless call.
