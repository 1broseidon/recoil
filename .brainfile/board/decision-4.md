---
id: decision-4
title: "Integration posture: hooks and skills before MCP"
type: decision
column: reference
position: 3
priority: medium
tags:
  - decision
  - integration
  - v0
parentId: epic-1
createdAt: "2026-05-11T02:26:37.232Z"
---

## Description
The CLI is the primitive. Instructions, hooks, and skills teach agents when to run wake/search/add. MCP should wait until CLI behavior, output, and evals are stable.

## Decision

Use instructions, hooks, and skills as the first integration layer.

## Agent Habit

```sh
recoil wake --max-chars 1600
recoil search "<topic>"
recoil add --agent <agent> --role decision "<memory>"
```

## Rationale

Hooks and skills solve adoption without changing storage. MCP is useful later,
but adding it too early risks freezing the wrong command semantics.

## Timing

Revisit MCP after:

- eval fixtures exist,
- wake/search/add behavior is stable,
- staleness handling has at least explicit lifecycle support,
- agent instructions have been dogfooded.
