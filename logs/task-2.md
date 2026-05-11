---
id: task-2
title: Implement eval harness for recall and stale demotion
priority: high
tags:
  - v0
  - eval
  - metrics
parentId: epic-1
contract:
  status: ready
  deliverables:
    - type: file
      path: cmd/eval.go
      description: Eval command or equivalent local runner
    - type: test
      path: internal/eval
      description: Tests for metric calculation and fixture parsing
  validation:
    commands:
      - make test
  metrics:
    readyAt: "2026-05-11T02:26:05.916Z"
createdAt: "2026-05-11T02:26:05.918Z"
completedAt: "2026-05-11T03:43:18.107Z"
updatedAt: "2026-05-11T03:43:18.107Z"
---

## Description
Add a local command or script that loads eval fixtures, runs recoil search/wake, and reports recall@k, MRR, empty-result accuracy, scope isolation, stale demotion, and latency.

Run this before adding more ranking or lifecycle semantics so the brainfile-less
loop is measurable. The harness should assume canonical truth comes from direct
Recoil memories and ordinary project docs, not `.brainfile/`.
