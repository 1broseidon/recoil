---
id: task-21
title: "search --explain: per-result score component breakdown"
column: todo
position: 10
priority: low
tags:
  - audit
  - ergonomics
  - observability
relatedFiles:
  - cmd/search.go
  - cmd/retrieval_lanes.go
  - internal/store/store.go
parentId: epic-1
createdAt: "2026-06-10T04:41:02.694Z"
---

## Description
The why line (cmd/retrieval_lanes.go:105) is categorical, not diagnostic. With many stacked boosts (bm25, claim-key/role SQL boosts, sourcequality priors, coverage bonus, RRF fusion weights) neither operator nor agent can see which prior fired when ranking is wrong. Add --explain to search (and wake) that emits per-result component scores. Pays off during tuning and operator debugging; pairs with the eval harness.
