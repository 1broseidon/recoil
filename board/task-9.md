---
id: task-9
title: Add 10k local performance benchmark
column: todo
position: 8
priority: medium
tags:
  - v0
  - benchmark
  - performance
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: test
      path: internal/store
      description: Benchmark data generator and search/wake timing harness
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:28:25.501Z"
---

## Description
Add a repeatable local benchmark for add/search/wake at 10k memories so Recoil's Cymbal-like latency target is measurable.
