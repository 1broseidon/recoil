---
id: task-25
title: Fix 3 pre-existing docs-heavy corpus failures (absent-graphql, absent-redis, contradiction-auth-current)
priority: medium
tags:
  - audit
  - eval
  - retrieval
relatedFiles:
  - eval/corpora/docs-heavy/cases.jsonl
  - internal/store/store.go
parentId: epic-1
createdAt: "2026-06-10T06:05:54.031Z"
completedAt: "2026-06-10T10:54:51.492Z"
updatedAt: "2026-06-10T10:54:51.492Z"
---

## Description
Discovered while gating task-17: ./recoil eval eval/corpora/docs-heavy/cases.jsonl fails 3/14 on both pre- and post-task-17 binaries, so these are pre-existing. Cases: absent-graphql and absent-redis (expected_empty cases leaking results) and contradiction-auth-current (stale/contradicted doc surfacing as current). Diagnose whether expectations or ranking are wrong; the absent-* cases suggest the OR candidate pool returns weak matches where empty is correct — possibly fixable with a minimum-relevance threshold on loose-pass results.
