---
id: epic-3
title: "Agent-experience pass: edit-time recall and hook quality"
type: epic
column: roadmap
position: 2
priority: high
tags:
  - roadmap
  - agent-flow
  - hooks
  - decisions
createdAt: "2026-09-23T02:59:02.733Z"
---

## Description
Findings from a review of recoil from the agent's side (chain.sh session, 2026-09-22). The agent opened a session in chain.sh and got recoil's SessionStart injection; the latest handoff was truncated mid-word while the fixed contract printed in full with a literal <agent> placeholder. Decisions only surface when the agent remembers to run `recoil check`, which exits 0 whatever the verdict.

Boundary for every child task: it must be worth building for a project that has none of the other chain tools installed. Recoil never calls, names or depends on ketch, cymbal or brainfile (see decision-5). Other tools compose with recoil through generic inputs (commands, paths, refs, metadata), never through recoil knowing them.

Biggest opportunity: decisions that surface at edit time, and that go stale when what they govern changes, without the agent having to remember a command.

## Log
- 2026-09-23T20:53:59.460Z: [claude] Closed as an epic (2026-09-23). Its bounded children stay on the board as standalone todo tasks; task-30 (executable predicates) is closed as won't do. Scope rule for what remains: plan-1.
