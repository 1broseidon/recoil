---
id: task-27
title: "SessionStart budget: never cut the latest handoff while the fixed contract prints in full"
column: todo
position: 3
priority: high
tags:
  - hooks
  - wake
  - handoff
  - budget
relatedFiles:
  - cmd/hook.go
  - cmd/instructions.go
createdAt: "2026-09-23T02:59:34.731Z"
---

## Description
Observed 2026-09-22 in chain.sh with `recoil hook remind --format=claude-code --max-chars 1200`: the injection was about 2.2K chars. The handoff.latest body was truncated mid-word ("references/t... [truncated]") while the ~1K-char agent contract (hookReminderText) printed in full after it. --max-chars applies to the wake layers only (hookWakeContext -> layeredMemoryBlocks); the contract is outside the budget.

Fix the priorities: the latest active handoff is the most valuable thing in the block, so give it first claim on the budget and drop lower-ranked memories before cutting it; if it must be cut, cut at a section boundary and say how to read the rest (`recoil show <id>`). Count the contract inside --max-chars, and offer a short contract form (a three-line wake/check/handoff reminder) once the project has been used for a while or via a flag.

Done when: a 2.5K handoff with --max-chars 1200 yields a readable, section-bounded handoff plus a pointer to the full memory, and the whole injection respects the budget.
