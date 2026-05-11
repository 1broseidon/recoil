---
schema: https://brainfile.md/v2/board.json
title: Recoil
agent:
  instructions:
    - Task files are individual .md files in board/
    - Completed tasks are in logs/
    - Preserve all IDs
    - Make minimal changes
    - Roadmap epics live in the roadmap column.
    - Reference decisions, ADRs, and research live in the reference column.
    - Executable work lives in todo or in-progress.
columns:
  - id: roadmap
    title: Roadmap
  - id: reference
    title: Reference
  - id: todo
    title: To Do
  - id: in-progress
    title: In Progress
types:
  decision:
    idPrefix: dec
    completable: true
  research:
    idPrefix: research
    completable: true
  adr:
    idPrefix: adr
    completable: true
---

# Recoil

Fast local memory recall for agents. Recoil makes the right local evidence
cheaper to retrieve than guessing.

> Note: Completing a task moves it to `logs/` via `brainfile complete`.
