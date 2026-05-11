---
id: epic-2
title: v1 Encrypted Peer Sync
type: epic
column: roadmap
position: 1
description: Extend Recoil from local-only recall to distributed local-first memory by replicating encrypted append-only events between authorized devices. Querying remains local; sync only moves encrypted facts so each device can rebuild its own SQLite/FTS index after decryption. Includes a v1.1 future reference for a dumb cloud relay that stores only encrypted envelopes and never performs search.
priority: medium
tags:
  - roadmap
  - v1
  - sync
  - local-first
  - encryption
subtasks:
  - id: epic-2-1
    title: Design syncable scope identity and device model
    completed: false
  - id: epic-2-2
    title: Define encrypted event envelope format
    completed: false
  - id: epic-2-3
    title: Add export/import for encrypted event bundles
    completed: false
  - id: epic-2-4
    title: Prototype folder sync backend
    completed: false
  - id: epic-2-5
    title: Materialize decrypted events into SQLite/FTS locally
    completed: false
  - id: epic-2-6
    title: Add revocation and future-key-rotation design notes
    completed: false
  - id: epic-2-7
    title: Document v1.1 dumb cloud relay as future reference
    completed: false
createdAt: "2026-05-11T01:00:34.271Z"
contract:
  status: ready
  deliverables:
    - type: design
      path: docs/sync-protocol.md
      description: Event log, envelope, scope, key, and replication protocol design.
    - type: file
      path: internal/sync
      description: Encrypted append-only event model and local apply pipeline.
    - type: file
      path: cmd/export.go
      description: Encrypted scope export for portable transfer and testing.
    - type: file
      path: cmd/import.go
      description: Encrypted scope import and materialization into the local store.
    - type: file
      path: cmd/sync.go
      description: Initial folder-based push/pull sync backend.
    - type: test
      path: internal/sync
      description: Deterministic sync, conflict, tombstone, and replay tests.
    - type: docs
      path: docs/cloud-relay-future.md
      description: v1.1 dumb cloud relay reference design.
  validation:
    commands:
      - make test
      - make build
  constraints:
    - Search and wake must remain local operations; no remote search dependency.
    - Sync format must be encrypted before transport and safe for dumb storage/relay backends.
    - Use append-only events as the replication primitive, not mutable database row sync.
    - Scopes are sync boundaries with independent authorization and keys.
    - Device pairing, revocation, tombstones, and purge events must be represented in the event model.
    - Design must work first over file/folder sync before any hosted relay exists.
    - "v1.1 cloud relay remains a future dumb pipe only: no plaintext, no FTS index, no embeddings, no summaries, and no search queries."
  metrics:
    readyAt: "2026-05-11T01:00:34.271Z"
---

## Description
Extend Recoil from local-only recall to distributed local-first memory by replicating encrypted append-only events between authorized devices. Querying remains local; sync only moves encrypted facts so each device can rebuild its own SQLite/FTS index after decryption. Includes a v1.1 future reference for a dumb cloud relay that stores only encrypted envelopes and never performs search.
