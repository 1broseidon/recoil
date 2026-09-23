---
id: task-31
title: "check: opt-in exit codes for verdicts"
column: todo
position: 7
priority: medium
tags:
  - check
  - cli
  - scripting
relatedFiles:
  - cmd/check.go
  - cmd/errors.go
parentId: epic-3
createdAt: "2026-09-23T02:59:35.553Z"
---

## Description
`recoil check` exits 0 for every verdict (use, review, use_replacement, ignore, no_decision), so scripts and hooks must parse output to act. Add `--exit-code` (default off, to keep current behaviour): 0 for use/no_decision/ignore, and distinct non-zero codes for review and use_replacement that do not collide with the error codes 1-6 in cmd/errors.go. Document them in MANUAL.md next to the error codes.
