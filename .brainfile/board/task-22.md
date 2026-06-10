---
id: task-22
title: "Cleanup: dead pool clamp, dead wake branch, duplicate maxInt"
column: todo
position: 11
priority: low
tags:
  - audit
  - tech-debt
  - cleanup
relatedFiles:
  - cmd/search.go
  - cmd/wake.go
  - cmd/retriever.go
parentId: epic-1
createdAt: "2026-06-10T04:41:03.138Z"
---

## Description
Paper cuts from audit: (1) cmd/search.go:280-286 pool clamp is dead code — pool<100→100 then pool>100→100, so pool is always exactly 100; either delete the arithmetic or scale with limit. (2) cmd/wake.go:420-423 'if fromQuery ... return 3' followed by unconditional 'return 3' — dead branch. (3) maxInt in cmd/retriever.go duplicates Go 1.21+ built-in max.
