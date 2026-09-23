---
id: task-29
title: "source_unchanged: resolve relative paths against the project root, not the cwd"
priority: medium
tags:
  - bug
  - decisions
  - predicates
  - staleness
relatedFiles:
  - cmd/decision_predicate.go
createdAt: "2026-09-23T02:59:35.146Z"
updatedAt: "2026-09-23T21:20:50.559Z"
completedAt: "2026-09-23T21:20:50.559Z"
---

## Description
evaluateSourceUnchanged (cmd/decision_predicate.go:294-297) only runs filepath.Clean on a relative source_path and reads it relative to the process working directory. A check run from a subdirectory, or a hook whose cwd is not the repo root, reports the decision as broken/source_unreadable instead of holds or violated. Resolve relative paths against the scope's project root (the same root .recoil/project.json lives in), and store paths project-relative at decide time.

Done when: the same decision evaluates identically from the repo root, a subdirectory, and a hook process.
