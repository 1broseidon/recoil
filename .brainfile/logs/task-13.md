---
id: task-13
title: decide/handoff auto-supersede previous active memory in same claim family
priority: high
tags:
  - audit
  - lifecycle
  - stale-memory
relatedFiles:
  - cmd/decide.go
  - cmd/handoff.go
  - cmd/supersede.go
parentId: epic-1
createdAt: "2026-06-10T04:40:15.344Z"
completedAt: "2026-06-10T04:49:09.519Z"
updatedAt: "2026-06-10T04:49:09.519Z"
---

## Description
cmd/decide.go runs AddMemory with the given claim key but never demotes the previous active decision in that family — running decide --claim-key x twice yields two active decisions and check degrades to multiple_current_decisions/ask_operator. Same for handoff: every handoff defaults to claim_key handoff.latest (cmd/handoff.go:111) but never supersedes the prior handoff, so actives pile up. Fix: on decide/handoff, find the existing active memory for the claim key and mark it superseded with bidirectional links (like cmd/supersede.go:86-93 does), with --no-supersede as escape hatch.
