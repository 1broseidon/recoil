---
id: task-9
title: Add 10k local performance benchmark
priority: medium
tags:
  - v0
  - benchmark
  - performance
parentId: epic-1
contract:
  status: done
  deliverables:
    - type: test
      path: internal/store
      description: Benchmark data generator and search/wake timing harness
  validation:
    commands:
      - make test
  metrics:
    pickedUpAt: "2026-05-11T05:01:48.196Z"
    reworkCount: 0
    deliveredAt: "2026-05-11T05:04:02.894Z"
    validatedAt: "2026-05-11T05:04:03.142Z"
    duration: 135
createdAt: "2026-05-11T02:28:25.501Z"
updatedAt: "2026-05-11T05:04:03.142Z"
completedAt: "2026-05-11T05:04:03.142Z"
---

## Description
Add a repeatable local benchmark for add/search/wake at 10k memories so Recoil's Cymbal-like latency target is measurable.
