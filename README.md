# recoil

Fast local operational memory for long-running agentic workflows.

recoil is a single Go binary backed by SQLite FTS5. It gives agents a sourced,
lifecycle-aware memory store for bounded projects — no cloud, no daemon, no
LLM in the required write path. Coding agents are the first wedge, but the
core loop is broader: **make the right local evidence cheaper to retrieve than
guessing.**

Use it when you need:

- An agent-facing memory CLI that survives session boundaries, with
  deterministic writes and sourced provenance on every retrieved chunk.
- A way to capture durable project decisions (ADRs, preferences, constraints)
  and let stale ones be demoted rather than deleted.
- A local search layer over your project's markdown — README, docs, design
  notes — with hash-tracked freshness so changed files supersede old chunks.
- A practical continuity layer for non-coding projects too: research, writing,
  planning, customer/account context, and other long-running bounded work.

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
- [Profiles And MCP](#profiles-and-mcp)
- [Embeddings and Hybrid Retrieval](#embeddings-and-hybrid-retrieval)
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
recoil setup
recoil status
```

Wake an agent session with bounded layered context:

```sh
recoil wake --max-chars 1600
```

Record durable context during work, then close the session with a handoff:

```sh
recoil remember --agent codex \
  "Use net/http with a 5s default timeout, not a third-party client."

recoil handoff --agent codex \
  --decision "Kept net/http for the HTTP client." \
  --next-step "Add timeout tests around gateway calls."

recoil decide --claim-key dependency.http-client \
  "We use net/http with a 5s default timeout, not a third-party client."

recoil decide --claim-key dependency.cache.redis \
  --stance rejects \
  --subject Redis \
  --holds-while "ops cost remains unjustified at current scale" \
  --recheck "Has scale or cost picture changed?" \
  "Decided against Redis for cache."
```

Search before assuming prior context:

```sh
recoil search "why http client"
recoil search "auth" --role adr --current
recoil check "add Redis for this cache"
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
recoil setup
recoil init
recoil status
recoil config

# Write
recoil remember "Auth tokens live in the keyring" --agent codex
recoil handoff --agent codex --next-step "Verify auth token migration."
recoil add "Prefers vim keybindings"
recoil add "Auth tokens live in the keyring" --agent codex --role decision
recoil decide --claim-key auth.token-storage "Auth tokens live in the keyring"
recoil decide --claim-key dependency.cache.redis --validity rejected \
  --holds-while "ops cost remains unjustified at current scale" \
  --recheck "Has scale or cost picture changed?" \
  "Decided against Redis for cache."
recoil decide --claim-key dependency.cache.redis --stance rejects --subject Redis \
  "Redis remains rejected for this cache while ops cost is unjustified."

# Read
recoil search "why did we change auth"
recoil check "add Redis for this cache"
recoil search "what is current auth" --profiles auto
recoil search "dependency.sqlite-driver"
recoil wake --max-chars 1600
recoil wake --include-decisions
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

# Profiles and MCP
recoil profile --entity "Caroline"
recoil mcp

# Channel relay and artifacts (experimental)
recoil relay serve --addr :8787 --data /data
recoil relay invite --data /data --channel agents --relay-url http://localhost:8787
recoil channel join http://localhost:8787/v1/invites/<token> --agent codex
recoil channel publish
recoil channel roster
recoil channel sync

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

# Resilience
recoil backup
recoil backup --out /path/to/synced-folder --max 5

# Quality gates
recoil eval
recoil eval eval/fixtures.jsonl
recoil eval --suite workflows
recoil eval --suite cli-hard
recoil eval --suite workflows --out eval/results
recoil repair
```

All commands support `--json` for programmatic use; scan commands also support
`--minimal` for tab-separated rows.

## How It Works

**Storage.** SQLite with FTS5 through `github.com/mattn/go-sqlite3` (CGO).
WAL, busy timeout, foreign keys on, deterministic public IDs, deterministic
redaction before hashing/persistence. The core local memory path has no daemon:
the binary opens the DB, does its work, and exits.

**Scope.** Three explicit scopes:

| Scope | Selection | Use for |
|---|---|---|
| project | default (inside an initialized project) | Project-specific decisions, ADRs, notes |
| user | `--user` | Cross-project preferences and personal facts |
| session | `--session <id>` | One-session scratch memory |

Inside an initialized project (`.recoil/project.json` present), the common
path needs no flags: `recoil wake`, `recoil search "..."`, `recoil remember "..."`.
If the current directory is not inside an initialized project, commands warn
on stderr and use a local fallback scope.

**Workflow shape.** Coding projects usually store dependency decisions, ADRs,
handoff notes, and mined project docs. Non-coding projects use the same
primitive for bounded work: research preferences, writing constraints,
customer/account context, travel planning, health/provider facts, or other
durable local evidence. Recoil is not trying to become a general personal
knowledge base; it works best when the scope is explicit and the evidence is
useful for the next agent session.

**Retrieval.** `search` uses FTS5 over content plus role, claim_key, source
agent, source path, and source_ref, with conservative ranking boosts for
exact metadata matches and decision-like roles. Filters: `--role`,
`--claim-key`, `--validity`, `--current`, `--historical`, `--since`,
`--before`, `--source-kind`, `--source`, `--agent`. `wake` self-refreshes
changed tracked project files before returning context, then both `wake` and
`search` group results into product lanes: Current Decisions, Remote Artifacts,
Project Docs, Recent Evidence, and Historical. Each result includes a short
`why` line explaining why it surfaced.

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

Decision memories can carry optional predicates that explain when a decision
applies. Date-bound decisions use `--valid-until <date>` and are stored as the
canonical predicate kind `valid_until`. Recoil evaluates a narrow deterministic
set itself (`valid_until`, `source_unchanged`, `claim_key_status`,
`package_version`, `tsconfig_value`, and `lint_config_value`) and stores
semantic or external predicates as review prompts. Unknown predicate status is
not a warning by itself; `recoil check` only asks for review when the current
request maps to a rejected/superseded decision, a broken deterministic
predicate, multiple current decisions, or an external predicate that gates the
action.

Current decisions can also carry a stance and subject. That lets `recoil check`
distinguish "use Redis" from "avoid Redis" against the same remembered
decision:

```sh
recoil decide --claim-key dependency.cache.redis --stance rejects --subject Redis \
  "Reject Redis for this cache because ops cost is not justified."
recoil check "use Redis for this cache"
recoil mark <memory-id> --stance rejects --subject Redis
```

```sh
recoil check "add Redis for this cache"
recoil check --claim-key dependency.cache.redis
recoil wake --include-decisions
```

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

Supported hook installers also add a session-end capture command where the
runtime exposes an end-of-session payload:

```sh
recoil hook install claude-code
recoil hook install codex
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
wake-safety failures, wrong-memory failures, scope-isolation failures, and
ranked-content failures, and latency.

```sh
recoil eval
recoil eval eval/fixtures.jsonl
recoil eval --suite embeddings --retrieval hybrid
recoil eval --suite workflows
recoil eval --suite session-evidence
recoil eval --suite decisions
recoil eval --suite workflows --out eval/results
```

The default eval is the fast deterministic gate. Workflow suites exercise
mined docs and selected session evidence for coding and non-coding continuity.
External academic benchmarks stay under `bench/`; large real-repo stress stays
under `stress/`.

The consolidation plan lives in [docs/P_SERIES.md](docs/P_SERIES.md): product
workflow gates belong in `recoil eval`, academic comparisons stay in `bench/`,
and large real-repo stress stays in `stress/`.

## Profiles And MCP

`recoil profile --entity NAME` builds a deterministic `entity_profile` memory
from current sourced evidence. `recoil search` uses profile retrieval in
`--profiles auto` mode by default, and skips profiles for exact-detail queries
such as commands, quotes, stack traces, or line numbers. Use
`--profiles on|off` to override that router.

`recoil mcp` runs a stdio Model Context Protocol server using the official
`github.com/modelcontextprotocol/go-sdk/mcp` Go SDK. It exposes
`recoil_search` and `recoil_wake` tools read-only by default; start with
`recoil mcp --allow-write` to expose `recoil_add`.

## Channel Relay And Artifacts

`recoil channel` and `recoil relay` are the experimental machine-to-machine
exchange layer for distributed agent memory. Local and adjacent-project memory
on one machine should stay fully local through normal Recoil scopes and search.
The channel/relay path is for sharing selected artifacts between machines.

The relay is intentionally dumb. It stores signed roster cards and signed
memory artifact events, but it does not search memory, merge databases, or own
truth. Recoil clients keep their own local memory projection.

Bootstrap or run a self-hosted relay:

```sh
recoil relay setup --data ./recoil-relay-data --channel agents --relay-url http://localhost:8787

docker build -t recoil-relay .
docker run -p 8787:8787 -v recoil-relay:/data recoil-relay
```

Create a one-time registration invite:

```sh
docker run --rm -v recoil-relay:/data recoil-relay \
  relay invite \
  --data /data \
  --channel agents \
  --relay-url http://localhost:8787
```

Join from another Recoil database:

```sh
recoil channel join http://localhost:8787/v1/invites/<token> --agent codex
```

Publish exact artifacts from the joined scope:

```sh
recoil channel publish --claim-key architecture.relay
recoil channel publish --id mem_abc123
recoil channel publish --since 2h --dry-run
```

Or let write verbs publish eligible artifacts automatically:

```sh
recoil config set channel.auto_publish guidance
recoil decide --claim-key architecture.relay "Use the relay outbox for agent-to-agent sharing."
```

Auto-publish modes are `off`, `guidance`, and `all-local`. The guidance mode
publishes claim-keyed decisions, ADRs, constraints, preferences, rules, and
handoffs by default. Failed publishes stay in a local durable outbox and are
flushed by later writes, `wake`, `search`, `check`, `handoff`, or explicit
channel commands.

Agents normally do not need a manual sync before acting on context: `wake`,
`search`, `check`, and `handoff` refresh joined channels with a short fail-soft
timeout before returning context or closing out. For explicit mid-session syncs
and outbox inspection, use:

```sh
recoil channel refresh
recoil channel outbox
recoil channel outbox flush
```

Inspect the channel's peer roster and artifact index, or operate the relay:

```sh
recoil channel roster
recoil relay status --data /data
recoil relay invite list --data /data
recoil relay member list --data /data
recoil relay doctor --data /data --relay-url http://localhost:8787
```

Each Recoil database keeps its own node identity, joined-channel registry, and
import dedupe table. Imported artifacts preserve remote provenance in
`metadata_json` and remain local evidence; operators can search, inspect,
promote, supersede, or ignore them like any other scoped memory.

The local filesystem channel transport remains useful for tests and local
harnesses, but the intended machine-to-machine V0 is the Docker relay.

## Embeddings and Hybrid Retrieval

Embeddings are an opt-in retrieval layer that **fuses with** FTS5 rather
than replacing it. The core experience still runs through SQLite FTS5,
structured metadata, lifecycle filters, and source freshness — embeddings
just add a semantic side channel for paraphrase-heavy queries where keyword
match alone falls short.

Two providers ship today:

- `local-hash-v1` — deterministic local hash-based "embedding," no network
  calls, useful for schema/plumbing tests and offline use.
- `openrouter` — real embeddings via OpenRouter (default model:
  `openai/text-embedding-3-small`). Requires `OPENROUTER_API_KEY`.

```sh
# Index with real embeddings
export OPENROUTER_API_KEY=sk-or-v1-...
recoil embed index --provider openrouter --model openai/text-embedding-3-small

# Hybrid search: FTS5 + cosine similarity, fused with Reciprocal Rank Fusion
recoil search "how do we store credentials" --hybrid

# Semantic-only search
recoil embed search "background remote sync" --provider openrouter
```

`recoil search --hybrid` is the production path: it runs FTS5 and embedding
similarity in parallel, fuses with RRF (k=60), and returns the top results.
All standard filters (`--role`, `--claim-key`, `--validity`, `--current`,
etc.) work identically on both legs. The bench harness measures the
retrieval-only impact in `bench/RESULTS.md` (R@5 0.972 → 0.981 on
LongMemEval_S, with the biggest gain on multi-evidence questions).

`eval/embeddings.jsonl` is intentionally separate from the default gate.
It contains paraphrase-heavy cases where plain FTS is expected to struggle,
so semantic and hybrid retrieval can be measured without weakening the core
FTS baseline.

## Status

recoil is at v0. The shape is settled and the core CLI is stable enough to
build agent workflows on, but expect rough edges and surface-level changes
before v1.

Stable today:

- Storage, deterministic IDs, redaction, FTS5
- Scope model and project init
- `add` / `search` / `wake` / `list` / `show` / `forget`
- `mark` / `supersede` / `decide` lifecycle commands
- Decision relevance/verdict checks (`recoil check`) and decision-aware wake
  context
- Project file mining with freshness via file hashes
- `.recoilignore` per-project skip patterns
- Agent hook installers (Claude Code, OpenCode, Codex)
- Eval harness (14/14 on the bundled fixtures, MRR 1.0)
- Workflow eval suite for coding, non-coding, session-evidence continuity,
  decision relevance, and P3 verdict maturity
- Deterministic entity profiles (`recoil profile`) with profile-aware search
- Read-only MCP stdio server (`recoil mcp`)
- JSON envelope (`{version, kind, data}`)

Explicit non-goals for v0:

- No cloud sync in the core local memory path
- No daemon in the core local memory path
- No hosted dashboard
- No embeddings in the required path
- No LLM-based extraction in the default write path
- No automatic rewriting of older memories

Experimental:

- Optional embeddings sidecar (`recoil embed`)
- Eval-driven retrieval modes (`fts`, `semantic`, `hybrid`)
- Channel relay/artifact exchange (`recoil channel`, `recoil relay`) between
  Recoil databases on different machines

## License

MIT. See [LICENSE](LICENSE).
