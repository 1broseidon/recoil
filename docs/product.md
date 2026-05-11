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
recoil list
recoil show <memory-id>
recoil forget <memory-id>
```

`wake` is the session boot command. It should orient an agent with layered,
sourced context rather than behave like an unstructured recent-memory dump.

Inside an initialized project, no scope flag means the current project. Use
`--user` for durable cross-project preferences and `--session <id>` for
one-session memories.

## Settled Decisions

Reference these Brainfile records rather than expanding new docs:

- `decision-2`: Product thesis, recall must be cheaper than guessing.
- `adr-1`: Local SQLite FTS5 with deterministic writes.
- `decision-3`: CLI default is current initialized project scope.
- `decision-4`: Hooks and skills before MCP.
- `decision-1`: Freshness model, supersession rather than deletion.
- `research-1`: MemPalace comparison takeaways.
- `decision-5`: Brainfile inspiration, optional typed source not a dependency.
- `research-2`: Brainfile protocol takeaways for Recoil.

## Sequential Work

The executable v0/v0.1 backlog lives as child tasks under `epic-1`:

1. `task-1`: Create retrieval eval fixtures including stale cases. (done)
2. `task-3`: Implement conservative project file miner. (done)
3. `task-4`: Improve wake into layered current context.
4. `task-5`: Add validity metadata and supersession links.
5. `task-6`: Add mark and supersede lifecycle commands.
6. `task-2`: Implement eval harness for recall and stale demotion.
7. `task-7`: Make search and wake stale-aware.
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
- A Brainfile source adapter is deferred until the generic, brainfile-less
  product loop is useful on its own.

This keeps Recoil universal and avoids accidentally making the development
workflow into a product dependency.

## Stale Memory Rule

Old evidence is not automatically bad. It becomes dangerous when it looks like
current truth.

Recoil should preserve rejected and superseded history, but retrieval must make
current guidance unmistakable. In practice:

- `wake` should favor active decisions, constraints, and handoffs.
- `search` should show current results first.
- rejected/superseded memories should be labeled as historical context.
- evals must include stale/superseded cases before ranking is tuned.

## Non-Goals For The Core Path

- No hosted service.
- No daemon.
- No cloud sync in v0.
- No LLM in the write path.
- No MCP before CLI semantics and evals are stable.
- No inferred knowledge graph before verbatim recall is solid.
