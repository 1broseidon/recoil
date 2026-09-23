---
id: plan-1
title: "Recoil scope: a stateless decision ledger"
type: plan
column: reference
position: 7
status: active
tags:
  - scope
  - product
  - chain
createdAt: "2026-09-23T20:54:10.228Z"
---

## Description
What recoil is after v0.3.0, and the test every new task must pass.

---

Recoil is a verb an agent calls mid-session, not a place it stores things. Each call opens one local SQLite store, answers, and exits. It sits next to ketch (what does the web say) and cymbal (what does this code do) and answers: what did we decide, and does it still hold?

## A task belongs on this board when

- It is one command or flag that answers at call time and exits.
- It works in a project with none of the other chain tools installed. Recoil never names ketch, cymbal or brainfile.
- Reads stay reads. Nothing turns `search`, `wake` or `check` into a write.
- Nothing needs a server, a daemon, a network peer or a background process. The optional embedding providers are the only network calls.
- Hooks are allowed because they are the same stateless call placed at the moment a decision matters: session start, before an edit, session end.

## Closed as out of scope (2026-09-23)

| Item | Why |
| --- | --- |
| epic-2 Encrypted peer sync | Sharing by another name; v0.3.0 removed sharing |
| task-10 Brainfile source adapter | Names another chain tool; `mine` already reads markdown |
| task-20 Retrieval tracking and review queue | Makes every read a write |
| task-30 Executable external predicates | Runs agent-stored commands; hashes and dates already cover staleness |

## What stays

Small fixes and flags that make the existing calls sharper: `check --path` with an edit-time guard hook, the SessionStart budget and placeholder fixes, project-root path resolution for predicates, opt-in exit codes on `check`, provenance flags on `handoff`, and git HEAD in provenance.
