---
id: task-14
title: Write-time near-duplicate and conflict surfacing in remember/decide output
priority: high
tags:
  - audit
  - lifecycle
  - ergonomics
relatedFiles:
  - cmd/remember.go
  - cmd/decide.go
  - cmd/check.go
parentId: epic-1
createdAt: "2026-06-10T04:40:15.777Z"
completedAt: "2026-06-10T04:55:16.971Z"
updatedAt: "2026-06-10T04:55:16.971Z"
---

## Description
Claim families only form when writers manually reuse exact claim keys; inferred keys are content slugs (cmd/remember.go:183-186) so same-topic memories phrased differently never link. Fix: at write time run a cheap FTS search over existing decision-role memories with overlapping significant tokens; if a near-match exists, emit related_claim_key / possible_conflict (with memory ID) in frontmatter and JSON so the agent can pass --supersedes. Also add a way to browse the claim-key namespace (recoil claims or recoil list --claims). Deterministic, no LLM — this is how families form organically and how contradictions get caught at the highest-leverage moment.
