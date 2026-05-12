# recoil

Fast local memory recall for coding agents and the humans who work with them.

recoil is a single Go binary backed by SQLite FTS5. It gives agents a sourced,
lifecycle-aware memory store that lives next to your code — no cloud, no
daemon, no LLM in the write path. The thesis is short: **make the right local
evidence cheaper to retrieve than guessing.**

Use it when you need:

- An agent-facing memory CLI that survives session boundaries, with
  deterministic writes and sourced provenance on every retrieved chunk.
- A way to capture durable project decisions (ADRs, preferences, constraints)
  and let stale ones be demoted rather than deleted.
- A local search layer over your project's markdown — README, docs, design
  notes — with hash-tracked freshness so changed files supersede old chunks.

## Contents

- [Install](#install)
- [Quick Start](#quick-start)
- [Why recoil](#why-recoil)
- [Commands at a Glance](#commands-at-a-glance)
- [How It Works](#how-it-works)
- [Lifecycle Model](#lifecycle-model)
- [Mining Project Files](#mining-project-files)
- [Agent Hooks](#agent-hooks)
- [Eval Harness](#eval-harness)
- [Optional Embeddings Sidecar](#optional-embeddings-sidecar)
- [Status](#status)
- [License](#license)

## Install

**Go** (requires CGO for SQLite FTS5):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/recoil@latest
```

**From source**:

```sh
git clone https://github.com/1broseidon/recoil
cd recoil
make build       # produces ./recoil
make install     # installs to $GOPATH/bin
```

The Makefile sets `CGO_ENABLED=1` and `CGO_CFLAGS+=-DSQLITE_ENABLE_FTS5`
automatically. FTS5 is required — there is no degraded fallback.

## Quick Start

Initialize recoil inside a project:

```sh
cd my-project
recoil init
recoil status
```

Wake an agent session with bounded layered context:

```sh
recoil wake --max-chars 1600
```

Record a durable decision before compaction or handoff:

```sh
recoil decide --claim-key dependency.http-client \
  "We use net/http with a 5s default timeout, not a third-party client."
```

Search before assuming prior context:

```sh
recoil search "why http client"
recoil search "auth" --role adr --current
recoil list --claim-key dependency.http-client --current
```

Mine project markdown so it becomes searchable with provenance:

```sh
recoil mine --dry-run
recoil mine
```

When something changes, supersede the old memory rather than deleting it:

```sh
recoil supersede mem_abc... \
  "We switched to a 10s timeout after the gateway slowdown on 2026-04-12."
```

## Why recoil

Coding agents have a recall problem at the moments where remembering would
save the most time: after compaction, at session boundaries, when switching
branches, when picking work back up days later. The usual fix is to dump
context into a fresh conversation; the usual cost is paraphrase-drift, fabric-
ated history, and slow startups.

recoil solves a narrower problem cheaply:

- **Single primitive.** A Go binary you can run from any shell, an agent hook,
  or a CI job. No service, no daemon, no API key.
- **Sourced answers.** Every retrieved chunk carries `source_path`,
  `source_ref`, file hash, and lifecycle state. Agents can show where evidence
  came from.
- **Lifecycle, not deletion.** Old decisions are superseded with links, not
  removed. Retrieval demotes stale guidance rather than burying it.
- **Eval-gated retrieval.** Ranking and filter behavior are pinned by a
  fixture suite that runs offline.
- **Agent-readable by default.** Output is frontmatter-plus-content; `--json`
  uses a stable `{version, kind, data}` envelope; `--minimal` gives TSV rows.

If you've used [cymbal](https://github.com/1broseidon/cymbal) for code
navigation, recoil sits in the same lane for project memory: a fast,
deterministic local CLI primitive that agents call instead of guessing.

## Commands at a Glance

```sh
# Bootstrap
recoil init
recoil status
recoil config

# Write
recoil add "Prefers vim keybindings"
recoil add "Auth tokens live in the keyring" --agent codex --role decision
recoil decide --claim-key auth.token-storage "Auth tokens live in the keyring"

# Read
recoil search "why did we change auth"
recoil search "dependency.sqlite-driver"
recoil wake --max-chars 1600
recoil list --role adr --validity active
recoil list --claim-key auth.token-storage --current
recoil show <memory-id>

# Lifecycle
recoil mark <memory-id> --validity rejected
recoil supersede <old-memory-id> "Replacement memory text"
recoil forget <memory-id>          # tombstone (recoverable)
recoil forget <memory-id> --destroy

# Mining
recoil mine --dry-run
recoil mine docs/
recoil mine session-evidence --dry-run

# Session evidence (opt-in)
recoil config set session-evidence.enabled true
recoil session-evidence ingest --agent codex --session-id sess_a8f3 --file transcript.json
recoil session-evidence list
recoil session-evidence forget sess_a8f3

# Agent integration
recoil hook remind
recoil hook install claude-code
recoil hook install opencode
recoil hook install codex

# Optional sidecars
recoil embed index
recoil embed search "background sync"

# Quality gates
recoil eval
recoil eval eval/fixtures.jsonl
recoil repair
```

All commands support `--json` for programmatic use; scan commands also support
`--minimal` for tab-separated rows.

## How It Works

**Storage.** SQLite with FTS5 through `github.com/mattn/go-sqlite3` (CGO).
WAL, busy timeout, foreign keys on, deterministic public IDs, deterministic
redaction before hashing/persistence. No daemon — the binary opens the DB,
does its work, and exits.

**Scope.** Three explicit scopes:

| Scope | Selection | Use for |
|---|---|---|
| project | default (inside an initialized project) | Project-specific decisions, ADRs, notes |
| user | `--user` | Cross-project preferences and personal facts |
| session | `--session <id>` | One-session scratch memory |

Inside an initialized project (`.recoil/project.json` present), the common
path needs no flags: `recoil wake`, `recoil search "..."`, `recoil add "..."`.
If the current directory is not inside an initialized project, commands warn
on stderr and use a local fallback scope.

**Retrieval.** `search` uses FTS5 over content plus role, claim_key, source
agent, source path, and source_ref, with conservative ranking boosts for
exact metadata matches and decision-like roles. Filters: `--role`,
`--claim-key`, `--validity`, `--current`, `--historical`, `--since`,
`--before`, `--source-kind`, `--source`, `--agent`. `wake` returns a layered
startup view — current context, durable decisions/constraints, recent
supporting evidence — bounded by `--max-chars`.

**Output.** Default is agent-readable frontmatter (the fields an agent needs
to cite) followed by content. `--json` returns a stable envelope:

```json
{ "version": "0.1", "kind": "search_result", "data": { ... } }
```

`--minimal` returns TSV rows for shell pipelines.

## Lifecycle Model

The hard problem with memory isn't storing it — it's making sure stale
evidence doesn't masquerade as current truth. recoil's answer is structured
lifecycle on every memory:

- **`validity`**: `active`, `historical`, `rejected`, `superseded`, `stale`,
  or `unknown`.
- **`claim_key`**: a stable handle for a claim family (e.g.
  `dependency.sqlite-driver`, `auth.token-storage`). Multiple memories can
  share a key over time; one is current.
- **`supersedes` / `superseded_by`**: explicit links between an old memory
  and its replacement.

`search` partitions results: active and unknown memories surface as current
guidance; rejected, superseded, stale, and historical matches appear in a
labeled history section. `wake` excludes historical states entirely so
startup context never presents old evidence as current.

```sh
# Mark a memory as rejected — keep the evidence, demote it
recoil mark mem_abc... --validity rejected

# Or supersede with a replacement and link both sides
recoil supersede mem_abc... "We switched back to net/http on 2026-04-12."
```

Old evidence is not automatically bad. It becomes dangerous when it looks
like current truth. recoil preserves history; retrieval makes the current
answer unmistakable.

## Mining Project Files

`recoil mine` ingests project markdown and text files as sourced memory
chunks with `source_path` and `source_ref` line ranges. It skips hidden and
tooling directories (`.git`, `.recoil`, `node_modules`, `vendor`) by default,
and honors a `.recoilignore` file with gitignore-shaped patterns for
per-project skips.

```sh
recoil mine --dry-run
recoil mine
recoil mine docs/
```

Mined memories track file hashes in the `sources` table. Re-mining a changed
file stales chunks that are no longer present; project-wide re-mines stale
chunks for deleted tracked files. Provenance is preserved so an agent can
quote the source line range directly.

For directories that should not enter memory (test fixtures, vendored docs,
generated content), add them to `.recoilignore`. For directories that should
be visible-but-not-ingested (the agent should know the directory exists, but
its contents are not project guidance), declare a single index memory by
hand:

```sh
recoil add --role note --claim-key source.fixtures.eval-corpora \
  --validity active \
  "eval/corpora/ contains synthetic stress fixtures — not project guidance."
```

## Session Evidence

Session Evidence is selected, redacted evidence from an agent session. It is
not transcript hoarding: recoil keeps compact load-bearing slices such as user
directives, explicit choices, rejected paths, completion summaries, and handoff
notes. Tool payloads, chatter, and unconfirmed assistant speculation are skipped.

It is opt-in per project:

```sh
recoil config set session-evidence.enabled true
```

Runtime adapters can pipe transcript payloads into:

```sh
recoil session-evidence ingest --agent codex --session-id sess_a8f3 --file -
```

Ingest redacts before writing anything to disk, writes compact JSONL under the
local recoil state directory, and mines those records with `source_kind` set to
`session_evidence`, `role: source`, and `validity: unknown`. Evidence keeps
session and turn provenance, but it has lower authority than explicit decisions
and fresh project docs. `wake` caps session evidence so recent sessions cannot
crowd out durable guidance.

Operators can inspect or purge a session:

```sh
recoil session-evidence list
recoil session-evidence show sess_a8f3
recoil session-evidence forget sess_a8f3
```

`forget` for session evidence is a privacy purge: it removes the compact
evidence file and hard-deletes the mined memories for that session.

## Agent Hooks

`recoil hook remind` prints a short memory primer that agent runtimes can
inject at session start. It is intentionally reminder-only: recoil does not
intercept tools or replace shell usage.

First-class installers are available for the agents recoil can manage safely:

```sh
recoil hook install claude-code           # ~/.claude/settings.json
recoil hook install opencode              # <user-config-dir>/opencode/plugins/
recoil hook install codex                 # ~/.codex/hooks.json

# Project-scoped variants
recoil hook install claude-code --scope project
recoil hook install opencode --scope project
recoil hook install codex --scope project

# Marked AGENTS.md fallback for Codex builds without native hook support
recoil hook install codex-agents
```

Installers are idempotent and uninstall removes only recoil-owned content,
leaving unrelated hooks intact.

## Eval Harness

`recoil eval` seeds a temporary database from `eval/fixtures.jsonl`, runs the
fixture cases through the same store and wake-layer code as the CLI, and
reports recall@k, MRR, empty-result accuracy, stale-demotion failures,
wake-safety failures, scope-isolation failures, and latency.

```sh
recoil eval
recoil eval eval/fixtures.jsonl
```

The eval is the gate that keeps ranking changes honest. Don't tune retrieval
without running it.

## Optional Embeddings Sidecar

Embeddings are intentionally **not** on the required path. The core
experience runs through SQLite FTS5, structured metadata, lifecycle filters,
and source freshness. Real embedding providers are kept behind a sidecar
that has to earn its weight on the eval before it can be promoted.

The initial provider is `local-hash-v1`, a deterministic local
embedding-like provider used to validate schema, indexing, and hybrid
retrieval mechanics without network calls or API keys.

```sh
recoil embed index
recoil embed search "background remote sync"
recoil eval eval/embeddings.jsonl --retrieval fts
recoil eval eval/embeddings.jsonl --retrieval semantic
recoil eval eval/embeddings.jsonl --retrieval hybrid
```

`eval/embeddings.jsonl` is intentionally separate from the default gate. It
contains paraphrase-heavy cases where plain FTS is expected to struggle, so
semantic and hybrid retrieval can be measured without weakening the core FTS
baseline.

## Status

recoil is at v0. The shape is settled and the core CLI is stable enough to
build agent workflows on, but expect rough edges and surface-level changes
before v1.

Stable today:

- Storage, deterministic IDs, redaction, FTS5
- Scope model and project init
- `add` / `search` / `wake` / `list` / `show` / `forget`
- `mark` / `supersede` / `decide` lifecycle commands
- Project file mining with freshness via file hashes
- `.recoilignore` per-project skip patterns
- Agent hook installers (Claude Code, OpenCode, Codex)
- Eval harness (14/14 on the bundled fixtures, MRR 1.0)
- JSON envelope (`{version, kind, data}`)

Explicit non-goals for v0:

- No cloud sync
- No daemon
- No hosted dashboard
- No MCP server (it can come after the CLI semantics are stable)
- No embeddings in the required path
- No LLM-based extraction in the default write path
- No automatic rewriting of older memories

Experimental:

- Optional embeddings sidecar (`recoil embed`)
- Eval-driven retrieval modes (`fts`, `semantic`, `hybrid`)

## License

MIT. See [LICENSE](LICENSE).
