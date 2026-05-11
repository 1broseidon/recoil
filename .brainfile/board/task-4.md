---
id: task-4
title: Improve wake into layered current context
column: todo
position: 2
priority: high
tags:
  - v0
  - wake
  - usability
parentId: epic-1
contract:
  status: ready
  deliverables:
    - type: file
      path: cmd/wake.go
      description: Layered wake selection and output
    - type: test
      path: cmd
      description: Wake ordering and budgeting tests
  validation:
    commands:
      - make test
  metrics:
    readyAt: "2026-05-11T02:26:06.376Z"
createdAt: "2026-05-11T02:26:06.379Z"
---

## Description
Make wake output prioritize active decisions, constraints, handoffs, and recent notes in L0/L1/L2-style sections while keeping sourced evidence blocks and hard max-char budgets.
