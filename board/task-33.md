---
id: task-33
title: Record git HEAD in decision and handoff provenance
column: todo
position: 9
priority: low
tags:
  - provenance
  - decisions
  - handoff
  - git
relatedFiles:
  - cmd/decide.go
  - cmd/handoff.go
  - internal/store/store.go
createdAt: "2026-09-23T02:59:35.951Z"
---

## Description
There is no commit field anywhere in the memory schema; git state is captured only by per-file hashes in mined metadata. When the project is a git repo, stamp the current HEAD (and a dirty flag) into metadata on decide and handoff, and show it in `show`/`check` output. Lets an operator or agent date a decision against code history ("decided at a1b2c3, 40 commits ago") without any other tool.
