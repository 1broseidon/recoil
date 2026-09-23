---
id: task-26
title: "Surface decisions at edit time: check --path and a PreToolUse guard hook"
column: todo
position: 2
priority: high
tags:
  - agent-flow
  - hooks
  - decisions
  - check
relatedFiles:
  - cmd/check.go
  - cmd/hook.go
  - cmd/decision_predicate.go
createdAt: "2026-09-23T02:59:34.520Z"
---

## Description
Today a decision only reaches the agent if the agent remembers to run `recoil check "<proposed action>"`. Agents skip it.

Add `recoil check --path <file>`: return active decisions and constraints whose anchors reference that file (source_path, a source_unchanged predicate path, or any path ref), with the usual verdicts. Then add `recoil hook guard --format=claude-code|codex` for PreToolUse on Edit|Write: read the tool input's file path, run the path check, inject the matching decisions as additionalContext, and print nothing when there are none. It never blocks by default.

Stands alone: any project that records decisions with file anchors gets them at the moment of editing, with no other tool involved. `recoil setup` should offer to install the guard next to the SessionStart hook.

Done when: editing a file that an active decision is anchored to shows that decision in the agent's context before the write; editing an unanchored file adds nothing and costs under 50ms.
