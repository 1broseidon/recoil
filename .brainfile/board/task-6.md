---
id: task-6
title: Add mark and supersede lifecycle commands
column: todo
position: 4
priority: high
tags:
  - v0.1
  - staleness
  - cli
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: file
      path: cmd/mark.go
      description: Mark command
    - type: file
      path: cmd/supersede.go
      description: Supersede command
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:26:06.852Z"
---

## Description
Add explicit commands for marking memories stale/rejected/historical and creating a new active memory that supersedes an old one.
