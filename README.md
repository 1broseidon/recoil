# recoil

Fast local memory recall for agents and humans.

Recoil is a local-first CLI memory tool: a single Go binary, SQLite FTS5,
deterministic writes, scoped recall, and agent-readable output by default.

Recoil exists because agents forget in the exact moments when remembering would
save the most time. It makes the right local evidence cheaper to retrieve than
to guess.

For the product story, settled decisions, and active Brainfile task sequence,
see [docs/product.md](docs/product.md).

## Build

Recoil uses SQLite FTS5 through CGO. Build and test it the same way as Cymbal:

```sh
make test
make build
make install
```

If you call Go directly, enable FTS5:

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build .
```

## V0 Commands

```sh
recoil init
recoil status
recoil config

recoil add "Prefers vim keybindings"
recoil add "We moved auth tokens into the keyring" --agent codex --role decision
recoil add --validity active --claim-key auth.token-storage "Auth tokens live in the keyring"
recoil decide --claim-key dependency.sqlite-driver "Recoil uses mattn/go-sqlite3 with FTS5"

recoil search "why did we change auth?"
recoil search "dependency.sqlite-driver"
recoil wake --max-chars 1600
recoil mine --dry-run
recoil mine
recoil eval
recoil eval eval/embeddings.jsonl --retrieval hybrid
recoil embed index
recoil embed search "background remote sync"

recoil show <memory-id>
recoil list --since 7d
recoil list --role adr --validity active
recoil list --claim-key dependency.sqlite-driver --current
recoil mark <memory-id> --validity rejected
recoil supersede <old-memory-id> "Replacement memory text"
recoil forget <memory-id>
recoil forget <memory-id> --destroy

recoil instructions codex
recoil hook remind
recoil hook remind --format=claude-code
recoil hook install claude-code --scope project
recoil hook install opencode --scope project
recoil hook install codex --scope project
recoil repair
```

Default output is frontmatter plus content. Use `--json` for a stable
versioned envelope shaped as `version`, `kind`, and `data`; use `--minimal` on
scan commands for tab-separated rows.

Brainfile is used for this repo's task board, but product truths should also be
kept in ordinary docs and direct Recoil memories so the core loop stays
brainfile-less.

## Agent Hooks

`recoil hook remind` prints a short memory primer that agent runtimes can
inject at session start. It is intentionally reminder-only: Recoil does not
block tools or try to replace normal shell usage.

```sh
recoil hook remind
recoil hook remind --format=json
recoil hook remind --format=claude-code
recoil hook remind --format=codex
```

First-class installers are available for the agents we can manage safely:

```sh
recoil hook install claude-code          # ~/.claude/settings.json
recoil hook install claude-code --scope project
recoil hook uninstall claude-code

recoil hook install opencode             # <user-config-dir>/opencode/plugins/recoil-opencode.js
recoil hook install opencode --scope project
recoil hook uninstall opencode

recoil hook install codex                # ~/.codex/hooks.json
recoil hook install codex --scope project
recoil hook uninstall codex

recoil hook install codex-agents         # AGENTS.md compatibility fallback
```

Claude Code uses a native `SessionStart` hook. OpenCode uses a managed plugin
that injects the reminder into the system prompt transform. Codex uses native
`SessionStart` hooks through `hooks.json`; enable Codex's `codex_hooks` feature
flag if your Codex build does not already have it on. `codex-agents` is kept as
a marked `AGENTS.md` compatibility fallback.

Installers are idempotent. Managed files carry a Recoil marker where the host
format supports it; native hook entries are removed by the exact Recoil command,
so uninstall leaves unrelated user hooks alone.

## Mining Project Files

`recoil mine` imports conservative markdown/text project files as sourced
memory chunks. It skips hidden and tooling directories such as `.git`,
`.recoil`, `.brainfile`, `node_modules`, and `vendor` by default.

```sh
recoil mine --dry-run
recoil mine docs/
```

Mined memories keep `source_path` and `source_ref` line ranges so agents can
show where local evidence came from. Re-mining tracks file hashes in the
`sources` table: changed files stale old chunks that are no longer present, and
project-wide re-mines mark chunks from deleted files stale.

## Eval Harness

`recoil eval` seeds a temporary database from `eval/fixtures.jsonl`, runs
fixture-local `search` and `wake` cases, and reports recall@k, MRR,
empty-result accuracy, stale-demotion failures, wake-safety failures, scope
leaks, and latency.

```sh
recoil eval
recoil eval eval/fixtures.jsonl
recoil eval eval/embeddings.jsonl --retrieval fts
recoil eval eval/embeddings.jsonl --retrieval semantic
recoil eval eval/embeddings.jsonl --retrieval hybrid
```

`eval/embeddings.jsonl` is intentionally separate from the default gate. It
contains paraphrase-heavy cases where plain FTS is expected to struggle, so
semantic and hybrid retrieval can be measured without weakening the core FTS
baseline.

## Optional Embeddings

Embeddings are an optional sidecar, not part of the required v0 path. The core
system still works through SQLite FTS5, structured metadata, lifecycle filters,
and mined source freshness.

```sh
recoil embed index
recoil embed search "mandatory project task board"
```

The initial provider is `local-hash-v1`, a deterministic local embedding-like
provider used to validate schema, indexing, and hybrid retrieval mechanics
without network calls or API keys. Real embedding providers can be added behind
the same provider/model sidecar table once eval data proves they help.

## Local Benchmarks

`make bench` runs the repeatable 10k-memory store benchmark for add, search,
and wake backing reads.

```sh
make bench
```

## Lifecycle Metadata

Memories can carry structured lifecycle fields:

- `validity`: `active`, `historical`, `rejected`, `superseded`, `stale`, or
  `unknown`.
- `claim_key`: stable claim family, such as `dependency.sqlite-driver`.
- `supersedes` / `superseded_by`: links between evidence records.

These fields are persisted and shown in memory output. `search` treats active
and unknown memories as current guidance while separating rejected, superseded,
stale, and historical memories into a labeled history section. `wake` excludes
historical states by default.

Use `mark` to update lifecycle metadata on existing evidence:

```sh
recoil mark <memory-id> --validity rejected
recoil mark <memory-id> --validity historical --claim-key dependency.sqlite-driver
```

Use `supersede` to create a replacement memory and link both sides:

```sh
recoil supersede <old-memory-id> "Recoil uses mattn/go-sqlite3 with FTS5."
```

Use `decide` for new durable decisions. It requires a claim key and writes an
active decision by default:

```sh
recoil decide --claim-key dependency.sqlite-driver "Recoil uses mattn/go-sqlite3 with FTS5."
```

`search` and `list` support structured filters:

```sh
recoil search "sqlite" --role adr --current
recoil list --role decision --validity active
recoil list --claim-key dependency.sqlite-driver
recoil list --historical
```

Natural search indexes content plus role, claim key, source agent, source path,
and source reference. Exact filters remain the preferred path for canonical
metadata lookups.

## Waking A Session

`recoil wake` prints bounded startup context in layers:

- `L0 Current Context`: handoffs, next-step notes, and query matches.
- `L1 Decisions And Constraints`: durable decisions, ADRs, preferences, and
  constraints.
- `L2 Recent Notes And Evidence`: recent supporting memories and mined source
  chunks.

The default text output keeps sourced evidence blocks under `--max-chars`.
The JSON `data` includes both `layers` and a flattened `results` list.

## Scopes

Recoil v0 keeps reads and writes single-scope:

- `--user`: durable operator preferences and personal facts.
- no scope flag: project memory for the nearest initialized Recoil project.
- `--project <path>`: explicit project memory override.
- `--session <id>`: one session's memory.

All memory commands default to project scope. Use `--user` for cross-project
preferences and `--session` for one-session memories. If the current directory
is not inside an initialized Recoil project, commands warn on stderr and use a
local fallback scope.

By default Recoil stores memories in the OS app-data directory. In sandboxed
agent sessions where that directory cannot be opened, Recoil falls back to
`.recoil/recoil.db` for initialized projects unless `--db` or `RECOIL_DB` is
set. This keeps `wake` and `search` usable inside workspace-only agents, but it
means the sandboxed session sees the project-local DB, not any existing global
app-data DB.

## V0 Non-Goals

- No cloud sync.
- No daemon.
- No hosted dashboard.
- No MCP server before the CLI is excellent.
- No embeddings in the required path; optional sidecars must stay eval-driven.
- No LLM-based extraction in the default write path.
- No inferred room/topic hard filters.
- No automatic rewriting of older memories.
