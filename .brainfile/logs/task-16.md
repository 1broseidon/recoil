---
id: task-16
title: Quarantine benchmark-specific query expansions and content-sniffing noise markers
priority: medium
tags:
  - audit
  - retrieval
  - tech-debt
relatedFiles:
  - internal/retrieval/signals.go
  - cmd/search.go
parentId: epic-1
createdAt: "2026-06-10T04:40:32.223Z"
completedAt: "2026-06-10T05:43:39.421Z"
updatedAt: "2026-06-10T05:43:39.421Z"
---

## Description
internal/retrieval/signals.go hardcodes LoCoMo/LongMemEval artifacts in production: ProfileText tags for basil/mint/power-bank/debate-team (lines 41-48), ExpandedQueryText legs for doctor/sibling/kitchen-appliance/bake/hike (102-128). These fire unpredictably on real data. Worse, cmd/search.go isNegativeEvidence (482-505) includes 'hosted sync service' — recoil's own README content baked into the ranker — which silently suppresses legitimate results on other projects. Fix: keep generic operational-doc intent expansion (security/contributing/release) in core; move personal-domain expansions to an eval-only build tag or loadable table; replace content-sniffing noise markers with a metadata flag set at mining/seeding time.
