---
id: task-10
title: Add optional Brainfile source adapter
column: todo
position: 9
priority: low
tags:
  - v0
  - mine
  - brainfile
  - operator-memory
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: file
      path: internal/mine/brainfile.go
      description: Brainfile v2 board/log discovery and record extraction
    - type: file
      path: cmd/mine.go
      description: Mine mode or source path handling for Brainfile records
    - type: test
      path: internal/mine
      description: Brainfile miner fixtures and tests
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:31:24.239Z"
---

## Description
Deferred optional adapter. Build Recoil brainfile-less first: Brainfile may be
used by the operator and agent for task management in this repository, but it
must not be part of the immediate product path or required project-memory
adapter.

Later, if the generic miner and lifecycle model are solid, import .brainfile
board records and logs as typed high-signal memories when a project explicitly
wants that adapter.

## Log
- 2026-09-23T20:53:57.456Z: [claude] Closed, won't do (2026-09-23). A brainfile adapter would make recoil name another chain tool, which the epic-3 boundary and decision-5 rule out. Brainfile boards are markdown; plain `recoil mine` already indexes them if a project wants that.
