---
id: task-10
title: Add optional Brainfile source adapter
column: todo
position: 9
priority: low
tags:
  - v0
  - mine
  - brainfile
  - operator-memory
parentId: epic-1
contract:
  status: draft
  deliverables:
    - type: file
      path: internal/mine/brainfile.go
      description: Brainfile v2 board/log discovery and record extraction
    - type: file
      path: cmd/mine.go
      description: Mine mode or source path handling for Brainfile records
    - type: test
      path: internal/mine
      description: Brainfile miner fixtures and tests
  validation:
    commands:
      - make test
createdAt: "2026-05-11T02:31:24.239Z"
---

## Description
Deferred optional adapter. Build Recoil brainfile-less first: Brainfile may be
used by the operator and agent for task management in this repository, but it
must not be part of the immediate product path or required project-memory
adapter.

Later, if the generic miner and lifecycle model are solid, import .brainfile
board records and logs as typed high-signal memories when a project explicitly
wants that adapter.

## Scope

When `.brainfile/` exists, read Brainfile v2 projects:

- `.brainfile/brainfile.md`
- `.brainfile/board/*.md`
- `.brainfile/logs/*.md`
- `.brainfile/logs/ledger.jsonl`

For each record, preserve:

- Brainfile document ID,
- document type,
- column/status,
- title,
- description/body,
- parentId,
- priority,
- tags,
- contract status and validation commands,
- source path,
- completed/log provenance.

## Retrieval Meaning

Brainfile records should map into Recoil as high-signal project memory:

- `decision`, `adr`, and `research` become reference evidence.
- active `task` and `epic` records become current project state.
- completed logs become historical evidence.

Do not overwrite Brainfile. Recoil indexes it as evidence.

## Non-Dependency Requirements

- Do not build this before the generic file miner, layered wake, and stale
  lifecycle path are useful without Brainfile.
- Do not require Brainfile CLI or libraries at runtime.
- Do not fail generic `recoil mine` when `.brainfile/` is absent.
- Do not make Brainfile-specific fields required in the core memory schema.
- Treat Brainfile as one source adapter alongside generic files and transcripts.
