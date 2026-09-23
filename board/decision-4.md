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
createdAt: "2026-05-11T02:26:37.232Z"
---

## Description
The CLI is the primitive. Instructions, hooks, and skills teach agents when to run wake/search/add. MCP should wait until CLI behavior, output, and evals are stable.

## Log
- 2026-09-23T20:54:00.557Z: [claude] Stale as of v0.2.0: the MCP server shipped (read tools by default, writes behind --allow-write). The CLI-first posture still holds; the 'MCP should wait' clause no longer does.
