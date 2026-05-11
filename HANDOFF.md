# Recoil Handoff

Date: 2026-05-11
Workspace: `/Users/george/Projects/personal/recoil`
Status: v0 CLI exists; task-1 eval fixtures, task-2 eval harness, task-3
generic file miner, task-4 layered wake, task-5 validity metadata, task-6
lifecycle commands, task-7 stale-aware retrieval, and the task-8 faceted
retrieval/source-freshness foundation are done. An optional embeddings sidecar
experiment is now in progress after the faceted foundation.

This doc is for a new chat session to continue without reconstructing the
thread.

## Start Here

Run these first:

```sh
cd /Users/george/Projects/personal/recoil
./recoil wake --max-chars 1600
brainfile list
brainfile show -t task-8
make test
./recoil eval eval/fixtures.jsonl
./recoil eval eval/embeddings.jsonl --retrieval hybrid
```

The default `recoil eval eval/fixtures.jsonl` target is 14/14 with MRR 1.0000,
so preserve that as the core regression target. The embedding fixture is a
separate experiment for paraphrase-heavy retrieval.

## Project Identity

The project is **Recoil**.

It was renamed from Reverb early in the thread. Current names are:

- CLI binary: `recoil`
- Go module: `github.com/1broseidon/recoil`
- project marker directory: `.recoil/`
- default DB env var: `RECOIL_DB`
- default DB path: `~/Library/Application Support/recoil/recoil.db`

Do not reintroduce `reverb` names.

The workspace is a git repo. Do not use destructive git commands.

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

Assume a normal project has no Brainfile. Canonical truths must live in direct
Recoil memories or ordinary project docs that `recoil mine` can ingest.

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
- No embeddings in the required path. Optional embedding sidecars are allowed
  only when evals prove they help.
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
recoil decide --claim-key <stable.key> "..."
recoil search "..."
recoil wake
recoil mine
recoil embed index
recoil embed search "..."
recoil show <memory-id>
recoil list
recoil forget <memory-id>
recoil repair

recoil instructions codex
recoil instructions claude-code
recoil instructions opencode
recoil hook remind
recoil hook install claude-code
recoil hook install opencode
recoil hook install codex
recoil hook install codex-agents
recoil version
```

Output:

- default: agent-readable frontmatter plus content
- `--json`: stable `version`, `kind`, and `data` envelope
- `--minimal`: tab-separated rows on scan commands

Scope behavior:

- no scope flag means nearest initialized project
- `recoil init` creates `.recoil/project.json`
- `--user` is explicit cross-project scope
- `--session <id>` is explicit one-session scope
- uninitialized project fallback warns on stderr
- if the default app-data DB is inaccessible in a sandboxed agent session,
  commands fall back to `.recoil/recoil.db` for initialized projects unless
  `--db` or `RECOIL_DB` is set

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
- source freshness for mined files through file hashes and stale chunk marking
- layered `wake` output with L0 current context, L1 decisions/constraints, and
  L2 recent notes/evidence
- isolated `recoil eval` harness for retrieval metrics against JSONL fixtures
- structured lifecycle metadata: `validity`, `claim_key`, `supersedes`,
  `superseded_by`
- lifecycle commands: `mark` and `supersede`
- stale-aware `search` and `wake`
- metadata-aware search over content, role, claim key, source agent, source
  path, and source reference
- structured filters on `search` and `list`: `--role`, `--claim-key`,
  `--validity`, `--current`, and `--historical`
- `recoil decide` for active claim-keyed decisions
- optional `memory_embeddings` sidecar table keyed by memory/provider/model
- `recoil embed index` and `recoil embed search` using local deterministic
  `local-hash-v1` provider
- `recoil eval --retrieval fts|semantic|hybrid` plus
  `eval/embeddings.jsonl` for paraphrase-heavy experiments

Current local DB:

```text
/Users/george/Library/Application Support/recoil/recoil.db
```

Current project marker:

```text
/Users/george/Projects/personal/recoil/.recoil/project.json
```

Recent `recoil status` showed:

- memory_count: 27
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
internal/eval/
eval/

.brainfile/
.recoil/
```

Product docs are intentionally compact. `docs/` now has:

```text
docs/product.md
```

Detailed build sequencing is in Brainfile records. Product truths should also
live in ordinary docs or direct Recoil memories so the product path stays
brainfile-less.

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
3. `task-2`: Implement eval harness for recall and stale demotion (done)
4. `task-5`: Add validity metadata and supersession links (done)
5. `task-6`: Add mark and supersede lifecycle commands (done)
6. `task-7`: Make search and wake stale-aware (done)
7. `task-8`: Expand project user and session scope tests
8. `task-9`: Add 10k local performance benchmark
9. `task-11`: Add agent hook system
10. `task-10`: Add optional Brainfile source adapter (deferred, low priority)

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

When a fact matters to Recoil users or agents, store it in ordinary docs or
direct Recoil memories. Do not hide canonical truth only in `.brainfile/`.

### Source Freshness

Long-term, Recoil memory should be a fast local search protocol over reliable
sources, not a static imported pile. Direct memories and ordinary docs are
first-class sources. Future adapters should use Cymbal-style JIT freshness:
check source fingerprints or cursors before `search`/`wake`, refresh dirty
source-derived chunks, prune or supersede deleted/changed evidence, and preserve
provenance.

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

`task-7` made retrieval lifecycle-aware:

- `search` over-fetches, partitions active/unknown memories from
  historical/rejected/superseded/stale memories, returns current results as the
  primary JSON/minimal surface, and labels historical matches separately in text
  output.
- `wake` filters historical/rejected/superseded/stale memories before layering,
  so startup context does not present old evidence as current guidance.
- `recoil eval eval/fixtures.jsonl` now passes 9/9:
  - recall_at_k: 1.0000
  - mrr: 1.0000
  - empty_result_accuracy: 1.0000
  - stale_demotion_failures: 0
  - wake_safety_failures: 0

## Recent Build Step

`task-6` added lifecycle commands:

- `recoil mark <memory-id> --validity rejected|stale|historical|active|superseded|unknown`
- `recoil mark <memory-id> --claim-key <key> --supersedes <id> --superseded-by <id>`
- `recoil supersede <old-memory-id> "<replacement memory>"`

`mark` preserves omitted lifecycle fields and updates only the fields requested.
`supersede` creates a new active replacement memory in the old memory's scope,
copies the old claim key unless `--claim-key` is provided, sets the new memory's
`supersedes` link, and marks the old memory `superseded` with `superseded_by`.

## Recent Build Step

`task-5` added first-class lifecycle metadata to the store:

- `validity`
- `claim_key`
- `supersedes`
- `superseded_by`

`AddMemory` can persist these directly or extract them from metadata JSON.
Existing databases migrate by adding the columns and backfilling lifecycle
values from metadata JSON where present. `UpdateLifecycle` provides the store
API that task-6 can use for mark/supersede commands.

The fields now appear in JSON and text memory output.

## Recent Build Step

`task-2` added `recoil eval`, backed by `internal/eval`.

The harness seeds a temporary DB from `eval/fixtures.jsonl`, runs `search` and
`wake` cases through the same store/wake-layer code as the CLI, maps generated
Recoil IDs back to fixture IDs, and reports recall@k, MRR, empty-result
accuracy, scope isolation failures, stale-demotion failures, wake-safety
failures, and latency.

Initial baseline from `./recoil eval eval/fixtures.jsonl` before task-7:

- passed: 5 / 9
- recall_at_k: 1.0000
- mrr: 0.8750
- empty_result_accuracy: 1.0000
- scope_isolation_failures: 0
- stale_demotion_failures: 3
- wake_safety_failures: 1

Task-7 closed these failures; keep 9/9 as the current regression target.

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

Text output keeps sourced blocks under `--max-chars`; JSON `data` includes both
`layers` and flattened `results`.

## Recent Build Step

Prerelease JSON output now uses a single top-level envelope:
`{"version":"0.1","kind":"...","data":...}`.

This removes the old `results.results` shape from summary commands such as
`mine --json` and `wake --json`; command-specific payloads still carry their
natural fields under `data`.

## Recent Build Step

`task-8` expanded scope coverage without changing runtime behavior.

CLI-level tests now cover default initialized project scope, explicit
`--project`, `--user`, `--session`, mutually exclusive scope flags,
uninitialized project warnings, and command-level isolation across project,
explicit project, user, and session memories in the same database.

Internal scope tests now cover project lookup from file paths, marker project
IDs and portable flags, root-specific uninitialized fallback IDs, stable user
IDs, and session ID trimming/rejection.

## Recent Review Fix

The P1/P2 review findings after task-8 are fixed in the working tree.

Search and wake now ask the store for current lifecycle rows directly instead
of fetching mixed current/history rows and filtering after `LIMIT`, so rejected,
superseded, stale, historical, or tombstoned memories cannot crowd active
guidance out of the bounded fetch window. Text search still queries historical
matches separately for the labeled history section.

Custom `metadata_json.validity` values are now tolerated as user metadata. Valid
Recoil lifecycle values are still backfilled from metadata, but invalid/custom
values such as `draft` are ignored instead of failing `add` or blocking legacy
DB open.

## Recent Build Step

Faceted retrieval and source freshness are now in place.

- `search` and `list` accept structured filters for role, claim key, validity,
  current lifecycle, and historical lifecycle.
- Search FTS now indexes content plus role, claim key, source agent, source
  path, and source ref, with conservative ranking boosts for exact metadata and
  decision-like roles.
- `recoil decide --claim-key <key> "..."` writes an active decision memory so
  agents do not have to remember the full `add --role decision --validity
  active --claim-key ...` incantation.
- Wake fetches startup context by quotas: handoff-ish source records first,
  ADR/decision/constraint/preference/rule records next, then recent evidence.
- `recoil mine` records file hashes in `sources`; re-mining a changed file
  marks old chunks stale when they are no longer present, and project-wide
  re-mines stale chunks for deleted tracked files.
- Eval fixtures now include metadata-only search, active ADR list, exact
  claim-key list, and ambiguous dependency/library retrieval cases.

Current target from `./recoil eval eval/fixtures.jsonl`: 14/14 pass with MRR
1.0000.

## Recent Build Step

Project commands now handle sandboxed agent sessions where
`~/Library/Application Support/recoil/recoil.db` cannot be opened.

If the default app-data DB fails with a filesystem access/open error and the
current directory is inside an initialized Recoil project, `openStore` falls
back to `.recoil/recoil.db`. Explicit `--db` and `RECOIL_DB` paths still remain
authoritative and do not fall back.

This fixes fresh Codex workspace sessions where `recoil wake` or `recoil search`
previously failed with `unable to open database file: no such file or
directory`.

## Recent Build Step

`task-11` added agent hook integration helpers:

- `recoil hook remind` now supports `text`, generic `json`, and
  `claude-code` SessionStart payloads.
- `recoil hook install claude-code` merges a marked SessionStart command into
  Claude settings at user or project scope.
- `recoil hook install opencode` writes a managed OpenCode plugin at user or
  project scope.
- `recoil hook install codex` writes a native Codex `hooks.json` SessionStart
  hook at user or project scope. Codex documents this under the `codex_hooks`
  feature flag.
- `recoil hook install codex-agents` remains available as a marked `AGENTS.md`
  compatibility fallback.

Installers are idempotent and uninstall removes only Recoil-owned content.

## Recent Build Step

`task-9` added a repeatable 10k-memory local benchmark.

The benchmark lives in `internal/store/store_bench_test.go` and is exposed via
`make bench`. It seeds a deterministic 10k-memory corpus with mixed active,
rejected, and stale lifecycle states, then measures:

- `BenchmarkStoreAddAt10K`
- `BenchmarkStoreSearch10K`
- `BenchmarkStoreWakeReads10K`

Baseline from `make bench` on Apple M4 with `-benchtime=50x`:

- add at 10k: 293151 ns/op
- search at 10k: 22508511 ns/op
- wake backing reads at 10k: 23110512 ns/op

## Next Build Step

`task-10` remains the only v0 board task, but it is intentionally low priority:
the optional Brainfile source adapter should stay deferred unless we explicitly
decide the brainfile-less core is strong enough.

Do not build the Brainfile source adapter before the generic miner, layered
wake, and stale lifecycle path are useful without Brainfile.

## Good New-Session Prompt

If starting a new chat, say:

> Continue Recoil from `/Users/george/Projects/personal/recoil`. Read
> `HANDOFF.md`, run `./recoil wake --max-chars 1600`, run `brainfile list`,
> run `./recoil eval eval/fixtures.jsonl`, and review whether to keep deferring
> `task-10` or add a new brainfile-less task. Keep the product path
> brainfile-less; canonical truths belong in direct memories or ordinary docs.

## Things To Avoid

- Do not build MCP next.
- Do not build sync next.
- Do not make Brainfile required.
- Do not build the Brainfile adapter before the brainfile-less core loop is
  useful.
- Do not make embeddings required; keep them optional and eval-driven.
- Do not tune ranking without checking `recoil eval`.
- Do not replace old memories by rewriting them as the default stale-memory
  solution.
- Do not use `git reset` or destructive git commands.
