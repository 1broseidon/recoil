# Recoil Product Notes

Recoil is a fast local memory recall CLI for coding agents.

Its product thesis is simple:

> Make the right local evidence cheaper to retrieve than guessing.

The board is the source of truth for work. Use Brainfile for sequencing:

```sh
brainfile list
brainfile show -t task-1
brainfile show -t decision-1
```

Brainfile is not the product substrate. Durable product and architecture truths
should also live in direct Recoil memories or ordinary project docs so Recoil is
designed for projects that have no Brainfile.

## Current Wedge

Recoil v0 is local coding-agent continuity inside an initialized project.

The minimum loop:

1. Operator runs `recoil init`.
2. Agent starts with `recoil wake`.
3. Agent searches before relying on prior project context.
4. Agent stores durable decisions before compaction or handoff.
5. Operator can inspect and correct memory with `list`, `show`, and `forget`.

If that loop is fast, sourced, and boring, richer retrieval and sync have a
foundation.

## Core Commands

```sh
recoil init
recoil wake --max-chars 1600
recoil search "<topic>"
recoil add --agent <agent> --role decision "<durable memory>"
recoil mine
recoil eval
recoil list
recoil show <memory-id>
recoil mark <memory-id> --validity stale
recoil supersede <old-memory-id> "<replacement memory>"
recoil forget <memory-id>
```

`wake` is the session boot command. It should orient an agent with layered,
sourced context rather than behave like an unstructured recent-memory dump.

Inside an initialized project, no scope flag means the current project. Use
`--user` for durable cross-project preferences and `--session <id>` for
one-session memories.

## Settled Decisions

Reference these Brainfile records for build sequencing, and promote durable
product truths into ordinary docs or direct Recoil memories:

- `decision-2`: Product thesis, recall must be cheaper than guessing.
- `adr-1`: Local SQLite FTS5 with deterministic writes.
- `decision-3`: CLI default is current initialized project scope.
- `decision-4`: Hooks and skills before MCP.
- `decision-1`: Freshness model, supersession rather than deletion.
- `decision-5`: Brainfile inspiration, optional typed source not a dependency.
- `research-2`: Brainfile protocol takeaways for Recoil.

## Sequential Work

The executable v0/v0.1 backlog lives as child tasks under `epic-1`:

1. `task-1`: Create retrieval eval fixtures including stale cases. (done)
2. `task-3`: Implement conservative project file miner. (done)
3. `task-4`: Improve wake into layered current context. (done)
4. `task-2`: Implement eval harness for recall and stale demotion. (done)
5. `task-5`: Add validity metadata and supersession links. (done)
6. `task-6`: Add mark and supersede lifecycle commands. (done)
7. `task-7`: Make search and wake stale-aware. (done)
8. `task-8`: Expand project/user/session scope tests.
9. `task-9`: Add 10k local performance benchmark.
10. `task-10`: Add optional Brainfile source adapter.

## Brainfile Inspiration

Brainfile is already an operator-focused task and memory system: local, typed,
inspectable, stable-ID, lifecycle-aware, and agent-readable.

Recoil should learn from that shape without requiring Brainfile. The
relationship should be:

- Recoil works for any project with plain `add`, `search`, `wake`, and generic
  file/transcript mining.
- For this repository, Brainfile is only the task-management protocol between
  operator and agent while Recoil is being built.
- Canonical product and architecture truths must live in direct Recoil memories
  or ordinary project docs, because a normal project has no Brainfile.
- A Brainfile source adapter is deferred until the generic, brainfile-less
  product loop is useful on its own.

This keeps Recoil universal and avoids accidentally making the development
workflow into a product dependency.

## Source Freshness Direction

Long-term, Recoil memory should be a fast local search protocol over reliable
sources, not a static imported pile. Direct memories and ordinary project docs
are first-class sources. Future source adapters should follow Cymbal's JIT
freshness posture: before `search` or `wake`, check source fingerprints or
cursors, refresh dirty source-derived chunks, prune or supersede deleted/changed
evidence, and preserve provenance.

## Stale Memory Rule

Old evidence is not automatically bad. It becomes dangerous when it looks like
current truth.

Recoil should preserve rejected and superseded history, but retrieval must make
current guidance unmistakable. In practice:

- memories carry `validity`, `claim_key`, `supersedes`, and `superseded_by`
  fields in the store.
- `wake` excludes rejected, superseded, stale, historical, and tombstoned
  memories by default.
- `search` shows active/unknown current guidance first and labels
  rejected/superseded/stale/historical matches as history in text output.
- evals must include stale/superseded cases before ranking is tuned.

## Non-Goals For The Core Path

- No hosted service.
- No daemon.
- No cloud sync in v0.
- No LLM in the write path.
- No MCP before CLI semantics and evals are stable.
- No inferred knowledge graph before verbatim recall is solid.
