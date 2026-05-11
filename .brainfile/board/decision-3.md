---
id: decision-3
title: "CLI default: current initialized project scope"
type: decision
column: reference
position: 2
priority: high
tags:
  - decision
  - scope
  - cli
  - v0
parentId: epic-1
createdAt: "2026-05-11T02:26:37.017Z"
---

## Description
Inside a folder with .recoil/project.json, omitted scope means this project. --user and --session stay explicit. Outside an initialized project, commands warn and use a local fallback scope.

## Decision

The common path for an agent inside a project is:

```sh
recoil wake
recoil search "topic"
recoil add "memory"
```

No `--project .` is required after `recoil init`.

## Rules

- `recoil init` creates `.recoil/project.json`.
- No scope flag means nearest initialized project.
- `--user` is explicit for cross-project operator preferences.
- `--session <id>` is explicit for one-session memories.
- If no initialized project exists, warn on stderr and use local fallback scope.

## Rationale

Agents follow the path of least resistance. The default should match the most
probable safe thing in a coding-agent workflow: this project.
