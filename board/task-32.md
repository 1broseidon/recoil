---
id: task-32
title: "handoff: accept --source-ref and --metadata like add/remember"
column: todo
position: 8
priority: medium
tags:
  - handoff
  - provenance
  - cli
relatedFiles:
  - cmd/handoff.go
parentId: epic-3
createdAt: "2026-09-23T02:59:35.748Z"
---

## Description
`recoil handoff` has no --source-path, --source-ref or --metadata, so a handoff cannot point at the work item it belongs to (an issue URL, a ticket ID, a task in whatever tracker the project uses). Add the same provenance flags add/remember/decide already take, and render any refs in wake output. Recoil stays tracker-agnostic: it stores the reference, it does not interpret it.
