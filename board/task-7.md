---
id: task-7
title: Make search and wake stale-aware
column: todo
position: 6
priority: high
tags:
  - v0.1
  - staleness
  - retrieval
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: file
      path: cmd/search.go
      description: Stale-aware output and ranking
    - type: file
      path: cmd/wake.go
      description: Wake filtering for superseded/stale states
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:26:07.081Z"
---

## Description
Once validity metadata exists, search should show current results first and labeled historical/rejected results separately; wake should exclude superseded memories by default.
