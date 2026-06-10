---
id: task-12
title: "Expand agent contract: instruct/hook text must cover search, check, decide, supersede"
priority: high
tags:
  - audit
  - ergonomics
  - agent-contract
relatedFiles:
  - cmd/instructions.go
  - cmd/hook.go
parentId: epic-1
createdAt: "2026-06-10T04:40:02.896Z"
completedAt: "2026-06-10T04:44:32.299Z"
updatedAt: "2026-06-10T04:44:32.299Z"
---

## Description
cmd/instructions.go agentInstructionText only mentions wake/remember/handoff. The injected hook text never tells agents to search before assuming, check before contradicting a remembered decision, decide --claim-key for durable choices, or supersede instead of re-remembering. Expand the contract to a ~10-line version covering the full lifecycle loop so agents actually exercise claim families and supersession. Cheapest accuracy lever in the audit.
