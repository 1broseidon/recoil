---
id: task-23
title: Mild recency prior for direct and session_evidence memories in default ranking
column: todo
position: 12
priority: medium
tags:
  - audit
  - retrieval
  - stale-memory
relatedFiles:
  - internal/store/store.go
parentId: epic-1
createdAt: "2026-06-10T04:41:12.266Z"
---

## Description
internal/store/store.go Search (417-428): created_at is only a tie-break. For direct and session_evidence memories recency correlates with truth — yesterday's handoff should comfortably beat an equal-bm25 note from months ago. Add a mild log-decay recency prior for those source kinds only; keep it off for file chunks (freshness there is correctly handled by hash refresh). Eval-gated against the workflows suite.
