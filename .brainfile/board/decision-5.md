---
id: decision-5
title: "Brainfile inspiration: optional typed source, not a dependency"
type: decision
column: reference
position: 6
priority: high
tags:
  - product
  - brainfile
  - operator-memory
  - v0
parentId: epic-1
createdAt: "2026-05-11T02:31:23.698Z"
---

## Description
Recoil should borrow Brainfile's operator-first lessons: typed records, stable IDs, visible lifecycle, contracts/provenance, and local inspectable state. Recoil must remain useful in any project without Brainfile installed or present.

## Decision

Recoil should not clone other local memory tools if a better local pattern is
visible in Brainfile, but Recoil must not depend on Brainfile.

Current build posture: build Recoil brainfile-less. In this repository,
Brainfile is allowed as task management between operator and agent, but
`.brainfile/` should not be treated as an immediate product adapter or as
special project memory for v0.

Assume a default project has no Brainfile. Durable truths that Recoil should
retrieve must be stored as direct Recoil memories or in ordinary project docs
that the generic miner can ingest. Brainfile must not become the crutch that
holds canonical decisions for the product path.

Brainfile proves that an operator-focused memory system can be:

- protocol-first,
- local and inspectable,
- typed,
- lifecycle-aware,
- agent-readable,
- explicit about ownership and validation.

## Recoil Implication

Recoil should treat high-signal structured records as first-class memory when
they exist:

- decisions,
- ADRs,
- research notes,
- tasks,
- contracts,
- completed logs,
- rules and agent instructions.

Structured records may rank above mined transcript noise later because the
operator or project already gave them structure. Brainfile is one possible
future adapter in this class, not the v0 path.

## Product Boundary

Brainfile is project work memory: what is planned, decided, assigned, blocked,
validated, or completed.

Recoil is universal local recall infrastructure: it indexes and retrieves the
right local evidence across source docs, transcripts, handoff notes, and any
structured local source adapters present in a project.

Brainfile support should compose with Recoil, not define Recoil.

The immediate product loop should prove itself on projects that have no
Brainfile at all.

Practically, this means product decisions and architecture facts that matter to
agents should be duplicated or promoted into normal docs or direct `recoil add`
records when they are expected to be retrievable.

## Non-Dependency Rule

A project with no `.brainfile/` folder must still get the full core experience:

- `recoil init`
- `recoil add`
- `recoil search`
- `recoil wake`
- generic file/transcript mining
- lifecycle and staleness controls
