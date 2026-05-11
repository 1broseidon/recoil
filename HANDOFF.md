# Recoil Handoff

Date: 2026-05-11
Workspace: `/Users/george/Projects/personal/recoil`
Status: v0 CLI exists; task-1 eval fixtures, task-3 generic file miner, and
task-4 layered wake are done; next build step is validity metadata.

This doc is for a new chat session to continue without reconstructing the
thread.

## Start Here

Run these first:

```sh
cd /Users/george/Projects/personal/recoil
./recoil wake --max-chars 1600
brainfile list
brainfile show -t task-5
make test
```

The next logical build step is `task-5`: add validity metadata and supersession
links. The current priority is immediate feature impact on projects with no
Brainfile.

## Project Identity

The project is **Recoil**.

It was renamed from Reverb early in the thread. Current names are:

- CLI binary: `recoil`
- Go module: `github.com/1broseidon/recoil`
- project marker directory: `.recoil/`
- default DB env var: `RECOIL_DB`
- default DB path: `~/Library/Application Support/recoil/recoil.db`

Do not reintroduce `reverb` names.

The workspace has no `.git` directory at the moment.

## Product Thesis

Recoil is a fast local memory recall CLI for coding agents.

The core thesis:

> Make the right local evidence cheaper to retrieve than guessing.

The v0 wedge is local coding-agent continuity inside an initialized project.

Minimum loop:

1. Operator runs `recoil init`.
2. Agent starts with `recoil wake`.
3. Agent searches before relying on prior project context.
4. Agent stores durable decisions before compaction or handoff.
5. Operator can inspect/correct memory with `list`, `show`, and `forget`.

## Important Product Boundaries

Recoil is universal. It must work for any project.

Brainfile is **not** a dependency. In this repo it is only:

- design inspiration,
- a task-management protocol between operator and agent while building Recoil.

The Brainfile adapter is explicitly deferred. Do not treat `.brainfile/` as an
immediate product adapter or special project memory for v0.

Core Recoil must work without Brainfile:

- `init`
- `add`
- `search`
- `wake`
- `list`
- `show`
- `forget`
- generic file mining
- transcript mining
- lifecycle/staleness controls

This clarification supersedes the earlier idea that Brainfile should be mined
before generic files.

## Non-Goals For The Core Path

- No hosted service.
- No daemon.
- No cloud sync in v0.
- No LLM call in the write path.
- No MCP before CLI semantics and evals are stable.
- No embeddings in the required path.
- No inferred knowledge graph before verbatim recall is solid.
- No automatic rewriting of older memories.

## Architecture Implemented

Language/runtime:

- Go 1.26.2
- Cobra CLI
- `github.com/mattn/go-sqlite3`
- SQLite FTS5 through CGO

Important: use the CGO SQLite path. This was chosen specifically to support
FTS5 correctly, inspired by Cymbal. Do not switch to a pure-Go SQLite driver
unless the user explicitly changes this decision.

Build/test:

```sh
make test
make build
```

`Makefile` sets:

```sh
CGO_ENABLED=1
CGO_CFLAGS+=-DSQLITE_ENABLE_FTS5
```

Last verified:

- `make test` passed
- `make build` passed earlier as `./recoil`
- `brainfile lint` passed

## Current CLI Surface

Implemented commands:

```sh
recoil init
recoil status
recoil config
recoil config path

recoil add "..."
recoil search "..."
recoil wake
recoil mine
recoil show <memory-id>
recoil list
recoil forget <memory-id>
recoil repair

recoil instructions codex
recoil instructions claude-code
recoil instructions opencode
recoil hook remind
recoil version
```

Output:

- default: agent-readable frontmatter plus content
- `--json`: stable JSON envelope
- `--minimal`: tab-separated rows on scan commands

Scope behavior:

- no scope flag means nearest initialized project
- `recoil init` creates `.recoil/project.json`
- `--user` is explicit cross-project scope
- `--session <id>` is explicit one-session scope
- uninitialized project fallback warns on stderr

## Current Store Behavior

Implemented:

- deterministic public IDs
- deterministic redaction before hashing/persistence
- SQLite schema for memories
- FTS5 external content table and triggers
- WAL/busy timeout/foreign keys
- tombstone vs destroy deletion
- list/search/wake/show APIs
- FTS repair
- conservative markdown/text project file mining through `recoil mine`
- layered `wake` output with L0 current context, L1 decisions/constraints, and
  L2 recent notes/evidence

Current local DB:

```text
/Users/george/Library/Application Support/recoil/recoil.db
```

Current project marker:

```text
/Users/george/Projects/personal/recoil/.recoil/project.json
```

Recent `recoil status` showed:

- memory_count: 13
- tombstone_count: 0
- fts5: true
- project_initialized: true

## Current Files

Primary files:

```text
README.md
HANDOFF.md
Makefile
go.mod
main.go
docs/product.md

cmd/
internal/config/
internal/redact/
internal/scope/
internal/store/
internal/mine/

.brainfile/
.recoil/
```

Docs were intentionally consolidated. `docs/` now only has:

```text
docs/product.md
```

Detailed product decisions are in Brainfile records, not scattered markdown.

## Brainfile Board

Brainfile is used for planning this project.

Board config:

```text
.brainfile/brainfile.md
```

Active records:

```text
.brainfile/board/*.md
```

Columns:

- `roadmap`
- `reference`
- `todo`
- `in-progress`

Roadmap:

- `epic-1`: v0 Local-Only Recoil Core
- `epic-2`: v1 Encrypted Peer Sync

Reference records:

- `decision-2`: product thesis, recall cheaper than guessing
- `adr-1`: local SQLite FTS5 with deterministic writes
- `decision-3`: current initialized project scope default
- `decision-4`: hooks and skills before MCP
- `decision-1`: freshness model, supersession rather than deletion
- `research-1`: MemPalace comparison takeaways
- `decision-5`: Brainfile inspiration is optional source, not dependency
- `research-2`: Brainfile protocol takeaways

Executable sequence under `epic-1`:

0. `task-1`: Create retrieval eval fixtures including stale cases (done)
1. `task-3`: Implement conservative project file miner (done)
2. `task-4`: Improve wake into layered current context (done)
3. `task-5`: Add validity metadata and supersession links
4. `task-6`: Add mark and supersede lifecycle commands
5. `task-2`: Implement eval harness for recall and stale demotion
6. `task-7`: Make search and wake stale-aware
7. `task-8`: Expand project user and session scope tests
8. `task-9`: Add 10k local performance benchmark
9. `task-10`: Add optional Brainfile source adapter (deferred, low priority)

## Settled Decisions

### CLI First

The CLI is the primitive. Hooks/instructions/skills teach agents when to call
it. MCP can come later after semantics, output, and evals are stable.

### Current Project Default

Inside an initialized project, the common path has no flags:

```sh
recoil wake
recoil search "topic"
recoil add "memory"
```

No `--project .` should be needed.

### Stale Memory

Stale memory is a core product risk.

Old evidence is not automatically bad. It becomes dangerous when it looks like
current truth.

Planned model:

- validity states: `active`, `historical`, `rejected`, `superseded`, `stale`,
  `unknown`, `tombstoned`
- claim keys such as `dependency.sqlite-driver`
- supersession links
- `wake` excludes superseded memories by default
- `search` shows active/current results first and stale/rejected history in a
  labeled section

Do not solve stale memory by deleting old evidence by default. Use
supersession, labeling, and retrieval ordering.

### Brainfile

Brainfile is useful inspiration: local, typed, inspectable, stable IDs,
lifecycle, contracts, rules, agent instructions.

But Recoil must not rely on Brainfile. Current build posture is brainfile-less:
generic files, transcripts, direct `add`, wake, and lifecycle controls first.
Treat Brainfile as task management between operator and agent in this repo, not
as a product adapter for now.

### MemPalace Comparison

MemPalace is stronger today as a complete memory app: mining, sweep, wake-up,
MCP, hooks, repair, benchmark story.

Recoil should win a different lane:

- fast Go shell primitive
- deterministic direct `add`
- implied project scope
- agent-readable output
- local SQLite FTS5
- lifecycle/trust controls

## Research/External Notes

MemPalace sources checked:

- installed local binary: `MemPalace 3.3.4`
- official repo: `https://github.com/MemPalace/mempalace`
- official CLI docs: `https://mempalaceofficial.com/reference/cli`
- benchmark docs and independent reproduction issue

Brainfile protocol sources checked:

- local repo: `/Users/george/Projects/core/brainfile/protocol`
- protocol docs: `https://brainfile.md/reference/protocol`
- CLI docs: `https://brainfile.md/reference/commands`

No need to redo this research unless the user asks.

## Recent Build Step

`task-3` added `recoil mine` for generic markdown/text evidence with
provenance, deterministic dedupe through existing `AddMemory`, project default
scope, and no LLM.

It intentionally skips `.brainfile/` and other hidden/tooling directories by
default so the v0 path stays brainfile-less.

## Recent Build Step

`task-4` made `recoil wake` layer startup context:

- `L0 Current Context`: handoffs, next-step notes, and query matches.
- `L1 Decisions And Constraints`: decisions, ADRs, preferences, rules, and
  constraint-like content.
- `L2 Recent Notes And Evidence`: recent supporting memories and mined source
  chunks.

Text output keeps sourced blocks under `--max-chars`; JSON includes both
`layers` and flattened `results`.

## Next Build Step

Build `task-5` next.

Goal: add validity metadata and supersession links so old evidence can be kept
without appearing as current guidance.

Do not build the Brainfile source adapter before the generic miner, layered
wake, and stale lifecycle path are useful without Brainfile.

## Good New-Session Prompt

If starting a new chat, say:

> Continue Recoil from `/Users/george/Projects/personal/recoil`. Read
> `HANDOFF.md`, run `./recoil wake --max-chars 1600`, run `brainfile list`,
> then start `task-5` by adding validity metadata and supersession links. Keep
> the product path brainfile-less; Brainfile is only task management here.

## Things To Avoid

- Do not build MCP next.
- Do not build sync next.
- Do not make Brainfile required.
- Do not build the Brainfile adapter before the brainfile-less core loop is
  useful.
- Do not add embeddings before local FTS/evals are proven.
- Do not tune ranking before eval fixtures exist.
- Do not replace old memories by rewriting them as the default stale-memory
  solution.
- Do not use `git reset` or destructive git commands; this workspace is not a
  git repo anyway.
