---
id: task-8
title: Expand project user and session scope tests
column: todo
position: 7
priority: high
tags:
  - v0
  - scope
  - tests
parentId: epic-1
contract:
  status: ready
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
createdAt: "2026-05-11T02:28:25.262Z"
---

## Description
Add deeper tests for default project scope, explicit --project, --user, --session, uninitialized fallback warnings, and scope isolation across stores.
