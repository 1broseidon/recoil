---
id: task-8
title: Expand project user and session scope tests
priority: high
tags:
  - v0
  - scope
  - tests
parentId: epic-1
contract:
  status: done
  deliverables:
    - type: test
      path: cmd/scope_test.go
      description: CLI-level scope resolution and warning tests
    - type: test
      path: internal/scope/scope_test.go
      description: Project/user/session scope behavior tests
  validation:
    commands:
      - make test
  metrics:
    readyAt: "2026-05-11T02:28:25.256Z"
    pickedUpAt: "2026-05-11T04:22:38.178Z"
    deliveredAt: "2026-05-11T04:24:56.398Z"
    validatedAt: "2026-05-11T04:24:56.398Z"
    reworkCount: 0
createdAt: "2026-05-11T02:28:25.262Z"
updatedAt: "2026-05-11T04:24:56.398Z"
completedAt: "2026-05-11T04:24:56.398Z"
---

## Description
Add deeper tests for default project scope, explicit --project, --user, --session, uninitialized fallback warnings, and scope isolation across stores.
