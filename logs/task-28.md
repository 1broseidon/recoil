---
id: task-28
title: Fix literal <agent> placeholder in the SessionStart contract
priority: medium
tags:
  - bug
  - hooks
  - instructions
relatedFiles:
  - cmd/hook.go
  - cmd/instructions.go
createdAt: "2026-09-23T02:59:34.944Z"
updatedAt: "2026-09-23T21:20:50.280Z"
completedAt: "2026-09-23T21:20:50.280Z"
---

## Description
cmd/hook.go:103 sets `hookReminderText = agentInstructionText("<agent>")`, so every SessionStart injection tells the agent to run `recoil remember --agent <agent> ...` with the placeholder left in. Derive the agent name from --format (claude-code -> claude, codex -> codex) or from an --agent flag on `hook remind`, and have `recoil setup`/`hook install` write that flag. If no name is known, phrase the line without a placeholder.
