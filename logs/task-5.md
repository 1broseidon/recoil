---
id: task-5
title: Add validity metadata and supersession links
priority: high
tags:
  - v0.1
  - staleness
  - schema
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: file
      path: internal/store
      description: Schema migration and APIs for validity metadata
    - type: test
      path: internal/store
      description: Validity and supersession persistence tests
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:26:06.621Z"
completedAt: "2026-05-11T03:50:38.017Z"
updatedAt: "2026-05-11T03:50:38.017Z"
---

## Description
Extend the memory model with status/validity, claim key, supersedes, and superseded_by data while preserving append-only evidence and existing rows.
