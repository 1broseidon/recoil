# recoil

Local-first memory for coding agents. One SQLite store holds verbatim memories
with provenance — who wrote each one, from where, in which scope, and whether it
still holds — and hands an agent a bounded block of the ones that matter at the
start of every session.

For people, it is the decision log that never goes stale. For agents, it
replaces re-reading the history with three calls: `wake` to start, `check`
before acting against a decision, `handoff` to close.

```console title="Install"
$ curl -fsSL https://recoil.sh/install | sh
```

```console title="Or hand it to your agent"
Install recoil and set it up in this repo for me.
1. Run: curl -fsSL https://recoil.sh/install | sh
2. Run `recoil setup` at the repo root and keep the hooks it offers.
3. Run `recoil wake --max-chars 1600` and tell me what it found.
From here on, start every session with `recoil wake`, run
`recoil check "<action>"` before changing a remembered decision, and
close with `recoil handoff --agent <you> --next-step "<next>"`.
```

## Overview

recoil stores memories, not summaries. Each is the text an agent or a person
wrote, with a role (`note`, `decision`, `constraint`, `preference`, `rule`,
`adr`, `handoff`, or `source` for indexed docs), a source agent, an optional
source path, a scope, and a validity state. Retrieval is SQLite FTS5 — no model
in the loop unless you opt into embeddings.

Decisions carry a claim key. A new decision in the same family supersedes the
old one, and `check` audits a proposed action against the family before an
agent goes ahead. Project docs are indexed at setup and refreshed by file hash,
so `wake` also surfaces the README and `docs/` an agent would otherwise re-read.

Everything is local and standalone by default. Sharing between machines or
agents is an opt-in memory tree behind a relay that only moves signed
artifacts. Hooks exist for Claude Code, OpenCode and Codex; the same surface is
available over MCP.

> Durable memory is authored by an agent or operator action. Automation may
> enrich, connect, publish, and review it. Automation may propose new memory,
> but proposals go to Review unless explicitly accepted.

## Install

One line on macOS and Linux. The script picks the archive for your platform,
verifies it against the release's `checksums.txt`, and installs into
`/usr/local/bin` when that is writable, otherwise `~/.local/bin`.

```console
$ curl -fsSL https://recoil.sh/install | sh
```

Pin a release or choose the directory; `RECOIL_VERSION` and
`RECOIL_INSTALL_DIR` do the same from the environment.

```console
$ curl -fsSL https://recoil.sh/install | sh -s -- --version v0.1.1 --bin-dir ~/bin
```

FTS5 is compiled into every published binary, so the requirement never shows
up as a flag you have to remember.

#### Windows — PowerShell

```console
> irm https://recoil.sh/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\recoil` and adds it to the user `PATH`.
`irm https://recoil.sh/uninstall.ps1 | iex` removes it and keeps your memories;
pass `-Purge` to delete the database too.

#### By hand — archives and checksums

Every release ships `recoil_<tag>_<os>_<arch>.tar.gz` for `linux/x86_64`,
`linux/arm64`, `darwin/arm64` and `darwin/x86_64`, a `.zip` for
`windows/x86_64`, and one `checksums.txt` covering all of them.

```console
$ TAG=$(curl -fsSL https://api.github.com/repos/1broseidon/recoil/releases/latest | grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)
$ OS=$(uname -s | tr '[:upper:]' '[:lower:]')
$ ARCH=$(uname -m); [ "$ARCH" = "aarch64" ] && ARCH=arm64
$ curl -fsSLO "https://github.com/1broseidon/recoil/releases/download/${TAG}/recoil_${TAG}_${OS}_${ARCH}.tar.gz"
$ curl -fsSLO "https://github.com/1broseidon/recoil/releases/download/${TAG}/checksums.txt"
$ grep "recoil_${TAG}_${OS}_${ARCH}.tar.gz" checksums.txt | shasum -a 256 -c -
$ tar xzf "recoil_${TAG}_${OS}_${ARCH}.tar.gz" && install -m755 recoil ~/.local/bin/recoil
```

#### Go — requires CGO for SQLite FTS5

```console
$ CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/recoil@latest
```

A binary built without FTS5 does not work; there is no degraded fallback.

#### From source — make sets the CGO flags

```console
$ git clone https://github.com/1broseidon/recoil && cd recoil
$ make build      # ./recoil
$ make install    # $GOPATH/bin/recoil
```

#### chain — the rest of the toolkit

recoil is one of the [chain.sh](https://chain.sh) tools. One command installs
the set:

```console
$ curl -fsSL https://chain.sh/bootstrap.sh | sh
```

## Quickstart

Run it inside a repository. `setup` marks the project, indexes its docs, and
installs hooks for the agents it finds; `--standalone` keeps sharing off.

```console capture
$ recoil setup --standalone --agent claude-code
---
tree: project
posture: standalone
sharing: off
evidence: off
project_root: /tmp/orbit
project_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
chunks: 2
hooks: 1
next_command: recoil wake
---
memory tree: project
posture: standalone
sharing: off
evidence: off

indexed 2 chunks from 2 files
detected agents: claude-code
hook claude-code: .claude/settings.json
next: recoil wake
```

Remember what an agent or a person just learned. The role is inferred from the
text; a plain observation is stored as a note, and the output says why.

```console capture
$ recoil remember --agent claude "The parser cache is keyed by file content hash, not mtime: editors rewrite mtimes on save without changing content."
---
id: mem_jtijf5wmyvvdoehp23sbjs3szu
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
role: note
claim_key: ""
inference_confidence: low
inference_reason: no decision, constraint, preference, or handoff signal; stored as note
duplicate: false
publish_mode: off
memory_shared: 0
publish_published: 0
publish_queued: 0
publish_duplicate: 0
publish_failed: 0
publish_skipped: policy
---
The parser cache is keyed by file content hash, not mtime: editors rewrite mtimes on save without changing content.
```

Decisions get a claim key so the family can be superseded and audited later.
`--stance` and `--subject` let `check` recognise a request that goes against
it.

```console capture
$ recoil decide --claim-key cache.backend --stance prefers --subject "parser cache backend" --agent claude "Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon."
---
id: mem_7vda2sxdlkgrurmicljaq5fq6k
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
created: 2026-09-21T18:08:04Z
role: decision
validity: active
claim_key: cache.backend
stance: prefers
subject: parser cache backend
predicate_status: unknown
supersedes: ""
superseded_by: ""
auto_superseded: ""
duplicate: false
publish_mode: off
memory_shared: 0
publish_published: 0
publish_queued: 0
publish_duplicate: 0
publish_failed: 0
publish_skipped: policy
---
Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon.
```

Every session starts with `wake`: a bounded block in lanes, each result with a
`why`. The two doc chunks came from `setup`.

```console capture
$ recoil wake --max-chars 1600
---
query: ""
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
result_count: 4
selected_count: 4
shown_count: 4
refreshed_sources: 0
staled_memories: 0
channel_imported: 0
channel_errors: 0
truncated: false
max_chars: 1600
peer_memory_received: 0
memory_shared: 0
pending_share: 0
channel_outbox_published: 0
channel_outbox_duplicate: 0
channel_outbox_failed: 0
channel_outbox_pending: 0
---
## Current Decisions
### mem_7vda2sxdlkgrurmicljaq5fq6k
score: 0.3500
created: 2026-09-21T18:08:04Z
role: decision
source_kind: direct
validity: active
claim_key: cache.backend
source_agent: claude
why: current decision with claim_key cache.backend

Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon.

## Project Docs
### mem_lq7i25mqrldbofqglixja2akx2
score: 1.8500
created: 2026-09-21T18:08:04Z
role: source
source_kind: file
validity: unknown
source_agent: recoil
source_path: README.md
source_ref: chunk 1 lines 1-3
why: project document chunk from README.md

# orbit

A parser for satellite two-line element sets with an on-disk cache.

### mem_yntgvdgzlkc2gcrl3bii46oesd
created: 2026-09-21T18:08:04Z
role: source
source_kind: file
validity: unknown
source_agent: recoil
source_path: docs/architecture.md
source_ref: chunk 1 lines 1-9
why: project document chunk from docs/architecture.md

# Architecture

The parser reads TLE files and keeps a cache of parsed elements so repeated
runs skip the slow checksum pass. Cache entries are keyed by file content hash.

## Cache

Entries live in a SQLite file in WAL mode. Redis was considered and rejected
because installs must work offline with no daemon.

## Recent Evidence
### mem_jtijf5wmyvvdoehp23sbjs3szu
score: 0.3500
created: 2026-09-21T18:08:04Z
role: note
source_kind: direct
validity: active
source_agent: claude
why: recent current memory in this scope

The parser cache is keyed by file content hash, not mtime: editors rewrite mtimes on save without changing content.
```

Before an agent acts against something remembered, it asks. The verdict is
`review`: the request would replace the subject of a decision that prefers it,
so the recommendation is to ask the operator rather than proceed.

```console capture
$ recoil check "replace the parser cache backend with Redis"
---
verdict: review
predicate_status: unknown
recommendation: ask_operator
reason: request_contradicts_decision
predicate_reason: ""
claim_key: cache.backend
decision_stance: prefers
decision_subject: parser cache backend
requested_action: replace_subject
advisory: ""
recheck: ""
channel_imported: 0
channel_errors: 0
peer_memory_received: 0
memory_shared: 0
pending_share: 0
channel_outbox_published: 0
channel_outbox_duplicate: 0
channel_outbox_failed: 0
channel_outbox_pending: 0
---
## Decision Check

claim_key: cache.backend
verdict: review
predicate_status: unknown
recommendation: ask_operator
decision_stance: prefers
decision_subject: parser cache backend
requested_action: replace_subject
matched_id: mem_7vda2sxdlkgrurmicljaq5fq6k
current_id: mem_7vda2sxdlkgrurmicljaq5fq6k

## Current Decision

## mem_7vda2sxdlkgrurmicljaq5fq6k
created: 2026-09-21T18:08:04Z
validity: active
claim_key: cache.backend
role: decision
source_kind: direct
source_agent: claude

Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon.
```

## Choosing a command

| I want to… | Use |
| --- | --- |
| Start a session with what matters | `recoil wake` |
| Find out whether something was decided | `recoil search "<topic>"` |
| Know if an action is safe against past decisions | `recoil check "<action>"` |
| Store an observation | `recoil remember "<text>"` |
| Record a decision that may later change | `recoil decide --claim-key <family> "<text>"` |
| Correct a memory that is now wrong | `recoil supersede <id> "<replacement>"` |
| Close a session for the next agent | `recoil handoff --next-step "<action>"` |
| See the decision families in scope | `recoil claims` |
| Pull a family verbatim into a prompt | `recoil export --claim-key-prefix <prefix>` |
| Retire a memory | `recoil forget <id>` |
| Change validity or links by hand | `recoil mark <id>` |
| Index docs without a full setup | `recoil mine` |
| See the tree, posture and sharing state | `recoil swarm` |
| Share memory with another tree | `recoil setup --relay <invite>` |
| Give an agent the contract | `recoil instruct <agent>` |
| Keep an agent on the contract | `recoil hook install <agent>` |
| Serve the same tools over MCP | `recoil mcp` |

## Commands

Every command takes `-d <path>` (or `RECOIL_DB`) to aim at another store and
`--json` for a structured envelope. Memory commands default to the project you
are in; `--user` selects the persistent user scope, `--project <path>` another
workspace, `--session <id>` a session scope.

### Sessions

#### setup — Bootstrap a project in one step

Marks the project (`.recoil/project.json`), indexes its docs, installs agent
hooks, and sets the sharing posture.

| Flag | Effect |
| --- | --- |
| `--standalone` | Local memory tree only |
| `--collaborative` | Join sharing with automatic publishing on |
| `--manual-share` | Join sharing, keep automatic publishing off |
| `--relay <invite>` | Relay invite URL to join; `--relay-agent <name>` names this node |
| `--agent <name>` | Hook to install; repeat or comma-separate. Auto-detected by default |
| `--hook-scope project` | Where hooks land: `project` (default) or `user` |
| `--no-hooks` | Skip hook installation |

#### wake — Bounded starter context

`wake [query]` prints up to `--max-chars` (default 1600) of memory in lanes,
current decisions first. `--include-decisions` appends a claim-keyed decision
trail, `--explain` adds per-result score components, `--minimal` prints
tab-separated rows. The filters below apply to `search`, `list` and `forget`
as well.

| Filter | Matches |
| --- | --- |
| `--current` / `--historical` | Lifecycle: current, or historical, rejected, superseded, stale and tombstoned |
| `--validity <state>` | One exact validity state |
| `--role <role>` / `--agent <name>` | Exact role or source agent |
| `--claim-key <key>` | One claim family |
| `--since 7d` / `--before 2026-09-01` | A date or a duration |
| `--source <substr>` / `--source-kind <kind>` | Source path substring; `direct`, `file`, `session_evidence` or `extracted_claim` |

#### handoff — Close a session for the next agent

Writes a structured handoff under the `handoff.latest` claim key, superseding
the previous one. `--next-step`, `--decision`, `--constraint` and
`--open-question` are repeatable; a free-text summary can come as an argument,
from `--file`, or from stdin with `--file -`.

```console
$ recoil handoff --agent claude --next-step "Land the per-project cache path, then re-run the cold-start benchmark" --open-question "Prune the cache by age or by size?"
---
id: mem_yr2bckupv2ebyrt3hki6g343ch
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
role: handoff
claim_key: handoff.latest
supersedes: ""
auto_superseded: ""
channel_imported: 0
channel_errors: 0
peer_memory_received: 0
memory_shared: 0
pending_share: 0
channel_outbox_published: 0
channel_outbox_duplicate: 0
channel_outbox_failed: 0
channel_outbox_pending: 0
publish_mode: off
memory_shared: 0
publish_published: 0
publish_queued: 0
publish_duplicate: 0
publish_failed: 0
publish_skipped: policy
---
## Next Steps
- Land the per-project cache path, then re-run the cold-start benchmark

## Open Questions
- Prune the cache by age or by size?
```

### Writing memory

#### remember — Store text with an inferred role

The role is inferred from decision, constraint, preference and handoff signals
in the text, and the inference is reported with its confidence. Override it
with `--role`, give a family with `--claim-key`, attach provenance with
`--source-path` and `--source-ref`, read long content with `--file`. Links use
`--supersedes` and `--superseded-by`.

#### decide — A decision with a claim key

`decide` is `remember` for decisions. The claim key is required; a new decision
automatically supersedes the previous active one in the family
(`--no-supersede` keeps both). `--stance prefers|rejects|requires|forbids` and
`--subject` describe what the decision is about so `check` can spot a
contradicting request.

```console
$ recoil decide --claim-key cache.backend --stance prefers --subject "parser cache backend" --agent claude "Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon."
---
id: mem_f3epegqcib4qepn47onte7nri2
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
created: 2026-09-21T06:33:07Z
role: decision
validity: active
claim_key: cache.backend
stance: prefers
subject: parser cache backend
predicate_status: unknown
supersedes: mem_7vda2sxdlkgrurmicljaq5fq6k
superseded_by: ""
auto_superseded: mem_7vda2sxdlkgrurmicljaq5fq6k
duplicate: false
publish_mode: off
memory_shared: 0
publish_published: 0
publish_queued: 0
publish_duplicate: 0
publish_failed: 0
publish_skipped: policy
---
Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.
```

A decision can carry a predicate for when it applies: `--valid-until <date>`
is deterministic, `--holds-while "<condition>"` is semantic,
`--predicate key=value,…` adds structured fields, and `--recheck "<question>"`
is what to ask the operator when the predicate needs review. `check` reports
the result as `predicate_status`.

#### supersede — Replace a memory that is now wrong

Creates the replacement, marks the old memory `superseded`, and links both.
The claim key and role default to the old memory's.

```console
$ recoil supersede mem_jtijf5wmyvvdoehp23sbjs3szu --agent claude "The parser cache is keyed by file content hash plus parser version, so a parser upgrade invalidates stale entries."
---
action: supersede
old_id: mem_jtijf5wmyvvdoehp23sbjs3szu
new_id: mem_w5seoglca7u74isihepwxxag4t
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
claim_key: ""
old_validity: superseded
new_validity: active
duplicate: false
publish_mode: off
memory_shared: 0
publish_published: 0
publish_queued: 0
publish_duplicate: 0
publish_failed: 0
publish_skipped: policy
---
The parser cache is keyed by file content hash plus parser version, so a parser upgrade invalidates stale entries.
```

#### mark — Change lifecycle metadata by hand

`mark <id>` sets `--validity` (`active`, `historical`, `rejected`,
`superseded`, `stale`, `unknown`), `--claim-key`, `--stance`, `--subject`, and
the `--supersedes` / `--superseded-by` links.

#### forget — Tombstone or purge

`forget <id> --reason "<why>"` tombstones one memory: it stays in the store,
leaves every current view, and shows up again with `list --include-deleted`.
`--destroy` hard-purges. The same filters as `wake` select memories in bulk;
`--dry-run` previews and `--force` confirms.

### Reading memory

#### search — FTS over the scope, history kept apart

Results come in the same lanes as `wake`, with historical matches in their own
section rather than dropped. `--hybrid` fuses FTS5 with embedding similarity
when an index exists (`recoil embed index`).

```console
$ recoil search "cache"
---
query: cache
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
retrieval_mode: fts
result_count: 5
history_count: 1
channel_imported: 0
channel_errors: 0
peer_memory_received: 0
memory_shared: 0
pending_share: 0
channel_outbox_published: 0
channel_outbox_duplicate: 0
channel_outbox_failed: 0
channel_outbox_pending: 0
---
## Current Decisions

## mem_f3epegqcib4qepn47onte7nri2
score: 1.0000
created: 2026-09-21T06:33:07Z
validity: active
claim_key: cache.backend
supersedes: mem_7vda2sxdlkgrurmicljaq5fq6k
role: decision
source_kind: direct
source_agent: claude
why: current decision with claim_key cache.backend

Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.

## Project Docs

## mem_toqzgisy5ms6ygdwy5c2upqfb2
score: 1.6000
created: 2026-09-21T06:33:07Z
validity: unknown
role: source
source_kind: file
source_agent: recoil
source_path: README.md
source_ref: chunk 1 lines 1-3
why: project document chunk from README.md

# orbit

A parser for satellite two-line element sets with an on-disk cache.

## mem_nbiasls4rskoavtnivpm6v6p7a
score: 1.0000
created: 2026-09-21T06:33:07Z
validity: unknown
role: source
source_kind: file
source_agent: recoil
source_path: docs/architecture.md
source_ref: chunk 1 lines 1-9
why: project document chunk from docs/architecture.md

# Architecture

The parser reads TLE files and keeps a cache of parsed elements so repeated
runs skip the slow checksum pass. Cache entries are keyed by file content hash.

## Cache

Entries live in a SQLite file in WAL mode. Redis was considered and rejected
because installs must work offline with no daemon.

## Recent Evidence

## mem_yr2bckupv2ebyrt3hki6g343ch
score: 1.0000
created: 2026-09-21T06:33:07Z
validity: active
claim_key: handoff.latest
role: handoff
source_kind: direct
source_agent: claude
why: matched query terms

## Next Steps
- Land the per-project cache path, then re-run the cold-start benchmark

## Open Questions
- Prune the cache by age or by size?

## mem_jtijf5wmyvvdoehp23sbjs3szu
score: 1.0000
created: 2026-09-21T06:33:07Z
validity: active
role: note
source_kind: direct
source_agent: claude
why: matched query terms

The parser cache is keyed by file content hash, not mtime: editors rewrite mtimes on save without changing content.

## Historical

## mem_7vda2sxdlkgrurmicljaq5fq6k
score: 1.0000
created: 2026-09-21T06:33:07Z
validity: superseded
claim_key: cache.backend
superseded_by: mem_f3epegqcib4qepn47onte7nri2
role: decision
source_kind: direct
source_agent: claude
why: matched query but lifecycle marks it historical, stale, rejected, or superseded

Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon.
```

#### check — Audit an action against decisions

`check "<action>"` finds the decision family the request touches and returns a
verdict, a recommendation and the reason. `check <memory-id>`,
`--claim-key <key>` and `--claim-key-prefix <prefix>` audit a family directly.
After the decision from the quickstart was superseded, the same question points
at the replacement:

```console
$ recoil check "replace the parser cache backend with Redis"
---
verdict: use_replacement
predicate_status: unknown
recommendation: use_current_decision
reason: matched_historical_decision
predicate_reason: ""
claim_key: cache.backend
decision_stance: ""
decision_subject: ""
requested_action: ""
advisory: ""
recheck: ""
channel_imported: 0
channel_errors: 0
peer_memory_received: 0
memory_shared: 0
pending_share: 0
channel_outbox_published: 0
channel_outbox_duplicate: 0
channel_outbox_failed: 0
channel_outbox_pending: 0
---
## Decision Check

claim_key: cache.backend
verdict: use_replacement
predicate_status: unknown
recommendation: use_current_decision
matched_id: mem_7vda2sxdlkgrurmicljaq5fq6k
current_id: mem_f3epegqcib4qepn47onte7nri2
replacement_id: mem_f3epegqcib4qepn47onte7nri2

## Current Decision

## mem_f3epegqcib4qepn47onte7nri2
created: 2026-09-21T06:33:07Z
validity: active
claim_key: cache.backend
supersedes: mem_7vda2sxdlkgrurmicljaq5fq6k
role: decision
source_kind: direct
source_agent: claude

Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.

## Matched Decision

## mem_7vda2sxdlkgrurmicljaq5fq6k
created: 2026-09-21T06:33:07Z
validity: superseded
claim_key: cache.backend
superseded_by: mem_f3epegqcib4qepn47onte7nri2
role: decision
source_kind: direct
source_agent: claude

Keep the parser cache in SQLite with WAL. Redis was rejected: installs must work offline with no daemon.
```

| Verdict | Meaning |
| --- | --- |
| `use` | The current decision applies; act on it |
| `use_replacement` | The request matched a superseded decision; use the one that replaced it |
| `review` | The request contradicts the current decision |

| Recommendation | When |
| --- | --- |
| `use_current_decision` | `use` and `use_replacement` |
| `ask_operator` | `review` against a decision that `prefers` or `rejects` |
| `block_action` | `review` against a decision that `requires` or `forbids` |

The request is read against the decision's subject. `use`, `add`, `enable`,
`switch to` and similar cues adopt it; `drop`, `remove`, `avoid`, `disable`
and similar remove it; `replace`, `switch from` and `move from` replace it;
`keep the decision` affirms it. A request that does not name the subject
reports `requested_action: unknown` and falls back to the family verdict.

#### claims — Claim families in scope

One row per family: key, number of memories, validity of the current one, and
its id.

```console
$ recoil claims
---
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
result_count: 1
---
cache.backend	2	active	mem_f3epegqcib4qepn47onte7nri2
```

#### show and list — One memory or many

`show <id>` prints a memory with its full frontmatter; the id can be a unique
prefix. `list` takes the `wake` filters, `--limit` (default 50) and
`--include-deleted`; `--minimal` is one tab-separated row per memory.

```console
$ recoil show mem_f3epegqcib4qepn47onte7nri2
---
id: mem_f3epegqcib4qepn47onte7nri2
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
created: 2026-09-21T06:33:07Z
validity: active
claim_key: cache.backend
supersedes: mem_7vda2sxdlkgrurmicljaq5fq6k
superseded_by: ""
role: decision
source_kind: direct
source_agent: claude
source_path: ""
source_ref: ""
---
Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.
```

```console
$ recoil list --current --minimal
mem_yr2bckupv2ebyrt3hki6g343ch	2026-09-21T06:33:07Z	claude	## Next Steps - Land the per-project cache path, then re-run the cold-start benchmark ## Open Questions - Prune the cache by age or by size?
mem_w5seoglca7u74isihepwxxag4t	2026-09-21T06:33:07Z	claude	The parser cache is keyed by file content hash plus parser version, so a parser upgrade invalidates stale entries.
mem_toqzgisy5ms6ygdwy5c2upqfb2	2026-09-21T06:33:07Z	recoil	# orbit A parser for satellite two-line element sets with an on-disk cache.
mem_nbiasls4rskoavtnivpm6v6p7a	2026-09-21T06:33:07Z	recoil	# Architecture The parser reads TLE files and keeps a cache of parsed elements so repeated runs skip the slow checksum pass. Cache entries are keyed by file content hash. ## Cac...
mem_f3epegqcib4qepn47onte7nri2	2026-09-21T06:33:07Z	claude	Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.
```

#### export — Whole claim families verbatim

For doctrine injection: every current memory in a family or prefix, verbatim,
in one block. `--historical` includes past memories.

```console
$ recoil export --claim-key-prefix cache.
---
kind: export
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
claim_key_prefix: cache.
lifecycle: current
count: 1
---
## cache.backend
id: mem_f3epegqcib4qepn47onte7nri2
created: 2026-09-21T06:33:07Z
validity: active
role: decision
source_kind: direct
source_agent: claude
supersedes: mem_7vda2sxdlkgrurmicljaq5fq6k

Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.
```

### Project and store

#### mine — Index project files

`setup` runs it once; `mine [path]` runs it again. Files are chunked and stored
as `source` memories with their path and line range, chunks already in the
store count as duplicates rather than being stored twice, and a `.recoilignore`
at the root lists paths to leave out. `--dry-run` previews.

```console
$ recoil mine --dry-run docs
---
root: /tmp/orbit/docs
scope: project
scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
dry_run: true
files_scanned: 1
files_skipped: 0
chunks: 1
added: 0
duplicates: 0
sources: 0
staled: 0
---
dry-run	would-add	docs/architecture.md	chunk 1 lines 1-9
```

#### status and swarm — Store and tree health

```console
$ recoil status
---
db_path: /tmp/orbit-home/.config/recoil/recoil.db
existed_before: true
memory_count: 7
tombstone_count: 0
fts5: true
project_scope_id: local:dbb8165a8ec8e32bfb255a15ecd75d69
project_root: /tmp/orbit
project_initialized: true
project_marker: /tmp/orbit/.recoil/project.json
---
ready
```

```console
$ recoil swarm
---
tree: project
posture: standalone
sharing: off
peers: 0
memory: 7
peer_memory: 0
evidence: off
review: 0
pending_share: 0
peer_memory_received: 0
memory_shared: 0
---
memory tree: project
posture: standalone
sharing: off
peers: 0
memory: 7
peer_memory: 0
evidence: off
review: 0
pending_share: 0
```

#### config — Effective configuration

Settings live in `.recoil/config.json` at the project root, layered over the
defaults. `config list` shows every key with its source, `config explain <key>`
one key, `config set` and `config unset` change a project override, and
`config path` prints the store path in use.

```console
$ recoil config list
---
path: /tmp/orbit/.recoil/config.json
count: 17
---
aging.window-days=30	default	Freshness window in days before note and handoff memories receive read-side aging demotion.
backup.dir=	default	Default destination directory for recoil backup snapshots.
backup.max=3	default	Maximum number of rotating backup snapshots to keep.
channel.auto_publish=off	project	Automatic channel publish mode: off, guidance, or all-local.
channel.auto_publish_include_remote=false	default	Allow auto-publishing memories imported from remote artifacts.
channel.auto_publish_requires_claim_key=true	default	Require a claim_key before auto-publishing guidance memories.
channel.auto_publish_roles=decision,adr,constraint,preference,rule,handoff	default	Comma-separated roles eligible for channel.auto_publish=guidance.
channel.jit_refresh=off	project	Implicit channel freshness mode: off, wake, context, or all.
channel.jit_refresh_timeout=2s	default	Maximum time for implicit channel refresh and outbox flush.
channel.outbox_flush_timeout=2s	default	Maximum time for automatic channel outbox flush after writes.
mine.exclude_paths=	default	Comma-separated path globs to exclude from mining even when they otherwise look important.
mine.follow_repo_symlinks=true	default	Follow symlinks that resolve inside the mined root for supported text files.
mine.include_hidden_operational=true	default	Include allowlisted hidden operational files such as .github/SECURITY.md without enabling every hidden file.
mine.include_paths=	default	Comma-separated path globs to force into mining, subject to file size and UTF-8 checks.
retrieval.mode=auto	default	Search retrieval mode: auto enables hybrid FTS+embeddings when a usable local embedding index exists; fts forces sparse search; hybrid requires a usable embedding index.
session-evidence.enabled=false	project	Enable mining session evidence captured by installed agent hooks.
session-evidence.min-chars=200	default	Minimum content length for session evidence files before ingestion selects them.
```

#### backup, migrate, repair — Maintenance

`backup` writes a consistent snapshot next to the database (or `--out`) and
rotates old ones (`--max`, default 3). `migrate` brings the schema up to date
and `repair` rebuilds the FTS index; both take `--dry-run`, and `migrate --age`
re-runs read-side aging.

#### embed, eval, profile, tray, version — The rest

`embed index` builds an embedding sidecar for the scope (`--provider local`
with `local-hash-v1`, or `openrouter`), and `search --hybrid` fuses it with
FTS5. `eval` runs the retrieval fixtures under `eval/`. `profile` builds a
deterministic entity profile from current memories. `tray` runs the system
tray companion. `version` prints version, commit and build date.

## Memory model

### Scopes

| Scope | Selected by | Holds |
| --- | --- | --- |
| project | the working directory, `--project <path>`, or `RECOIL_PROJECT` | Decisions, notes and docs for one repository; worktrees share it |
| user | `--user` | What follows you across projects |
| session | `--session <id>` | Context for one run |

All scopes live in one store. The project is marked by `.recoil/project.json`
and its id is derived from the repository, so clones on other machines line up
when sharing is on.

### Validity

| State | Meaning |
| --- | --- |
| `active` | Current |
| `superseded` | Replaced by a linked newer memory |
| `historical` | True once, kept for the record |
| `rejected` | Considered and turned down |
| `stale` | Its source file changed since it was indexed |
| `unknown` | Not asserted; the default for indexed docs |

Tombstoned memories (`forget`) are hidden from every current view. Notes and
handoffs older than `aging.window-days` (default 30) are demoted at read time,
never rewritten.

### Claim keys and supersession

A claim key names a family: `cache.backend`, `voice.tone`, `handoff.latest`.
`decide` supersedes the previous active decision in its family, `supersede`
links any two memories explicitly, `mark` edits the links. `claims`, `check`
and `export` work on families, and `--claim-key-prefix voice.` selects a whole
tree of them.

### Wake lanes

| Lane | What lands there |
| --- | --- |
| Current Decisions | Active decisions, newest first |
| Peer Memory | Memory received from other trees when sharing is on |
| Project Docs | Indexed file chunks, root docs first |
| Recent Evidence | Recent notes, handoffs and session evidence |
| Historical | Matches that lost their validity; `search` keeps them in their own section |

Every result carries a `why` line.

## Sharing

Sharing is off until you ask for it. `setup --relay <invite>` joins a memory
tree through a relay: a dumb server that stores signed artifacts and a roster,
never the database. `--collaborative` publishes eligible memories automatically
(`channel.auto_publish=guidance` shares decisions, ADRs, constraints,
preferences, rules and handoffs that carry a claim key; `all-local` shares
every local memory), while `--manual-share` joins but leaves publishing to
`channel publish`. `swarm --refresh` sends pending shares and pulls peer memory
before printing the card.

| Command | What it does |
| --- | --- |
| `channel join` / `status` / `roster` | Join a channel, list joined channels, show peers and the artifact index |
| `channel publish` / `refresh` / `sync` | Push current memories, pull new peer artifacts, replay them as remote evidence |
| `channel outbox` | Inspect pending automatic publishes |
| `relay setup` / `serve` | Bootstrap a data directory and serve it (`:8787` by default) |
| `relay invite` / `member` / `channel` | One-time invites, roster membership, channels |
| `relay status` / `doctor` | Footprint and health, with likely operator fixes |

The repository's `Dockerfile` builds the relay image; its default command is
`relay serve --addr :8787 --data /data`.

## For agents

recoil was built to be called by something that isn't a person.

### The contract

`instruct <agent>` prints the short contract for an agent's instructions file.
It names the agent so its memories carry the right `source_agent`.
(`instructions` is an alias kept for hooks that already call it.)

```console
$ recoil instruct claude-code
# Recoil memory contract for claude-code

Your workspace has a memory tree. Agents leave durable intent. Recoil keeps it fresh, shared, and cleaned up.

- Start work with `recoil wake --max-chars 1600`.
- Before assuming prior context or making a claim about project history, run `recoil search "<topic>"`.
- Before acting against or changing a remembered decision, run `recoil check "<proposed action>"` and respect its verdict (use / review / use_replacement).
- When durable intent is explicit, write it with `recoil remember --agent claude-code "<memory>"`.
- For durable decisions with a stable subject, prefer `recoil decide --claim-key <family> "<decision>"` so supersession and contradiction checks work.
- When something previously remembered is now wrong, use `recoil supersede <old-id> "<replacement>"` instead of writing a duplicate memory.
- End the session or compacting window with `recoil handoff --agent claude-code --next-step "<next action>"`.

Recoil shares eligible memory automatically when this workspace is collaborative. If you find yourself wanting to share something manually, note it in handoff so the rules can be tuned.
Treat Recoil output as sourced working context with IDs and provenance.
```

### Hooks

`hook install <agent>` wires the contract into a runtime so it survives a full
context; `--scope project` keeps it in the repository, `--dry-run` shows the
change. `hook remind` prints the reminder block for any other runtime, with
`--format text|json|claude-code|codex` and bounded wake context included by
default.

| Agent | Installed as |
| --- | --- |
| `claude-code` | Native SessionStart and SessionEnd hooks in Claude settings |
| `opencode` | A managed OpenCode plugin |
| `codex` | Native SessionStart and Stop hooks in `hooks.json` |
| `codex-agents` | A managed block in `AGENTS.md`, for runtimes without hooks |

### MCP

`recoil mcp` serves the same surface over stdio. Read-only by default:
`recoil_search`, `recoil_wake`, `recoil_check`, `recoil_list`, `recoil_claims`
and `recoil_export`. `--allow-write` adds `recoil_add`, `recoil_remember` and
`recoil_handoff`. Scope resolves the same way as on the command line, so
`RECOIL_PROJECT` aims a server whose working directory you don't control.

### Frontmatter, JSON, or rows

The default output is YAML frontmatter followed by content: metadata an agent
can parse, then text it can read. `--json` wraps the same data in an envelope
with a `version` and a `kind`; errors use the envelope with `kind: "error"`,
and the exit code still tells the story.

```console
$ recoil show mem_f3epegqcib4qepn47onte7nri2 --json
{
  "version": "0.1",
  "kind": "show_result",
  "data": {
    "id": "mem_f3epegqcib4qepn47onte7nri2",
    "hash": "2ec8f21a024079023dbcfb9b327db14695a6b10dac48d9c04009f664b3e688d4",
    "role": "decision",
    "content": "Parser cache moves to a per-project SQLite file under .cache/ so worktrees stop sharing one cache. Still SQLite, still no daemon.",
    "source_kind": "direct",
    "source_agent": "claude",
    "scope_kind": "project",
    "scope_id": "local:dbb8165a8ec8e32bfb255a15ecd75d69",
    "project_id": "local:dbb8165a8ec8e32bfb255a15ecd75d69",
    "metadata_json": "{\"stance\":\"prefers\",\"subject\":\"parser cache backend\"}",
    "validity": "active",
    "claim_key": "cache.backend",
    "supersedes": "mem_7vda2sxdlkgrurmicljaq5fq6k",
    "created_at": "2026-09-21T06:33:07Z"
  }
}
```

```console
$ recoil show nope --json
{"version":"0.1","kind":"error","error":{"code":"NOT_FOUND","message":"memory not found"}}
→ exit 3
```

`--minimal` on `wake`, `search` and `list` prints one tab-separated row per
memory: id, created, agent, content.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | OK |
| 1 | Generic failure |
| 2 | Validation: a bad flag, a missing argument, an invalid value |
| 3 | Not found |
| 4 | Upstream: the database, an embedding provider, the network |
| 5 | Failed precondition, such as an uninitialised project or a disabled feature |
| 6 | Cancelled |

### Bootstrap files

Two files on this domain are written for agents rather than people:
[/llms.txt](/llms.txt) is the short index and [/llms-full.txt](/llms-full.txt)
is this manual as plain Markdown.

## Notes

### FTS5 is mandatory

Published binaries have it compiled in. A Go build needs
`CGO_CFLAGS="-DSQLITE_ENABLE_FTS5"` and `CGO_ENABLED=1`; `make` sets both.

### Where the store lives

One database in your OS config directory: `~/.config/recoil/recoil.db` on
Linux, `~/Library/Application Support/recoil/recoil.db` on macOS,
`%AppData%\recoil\recoil.db` on Windows. `-d <path>` or `RECOIL_DB` aims a
command at another one, and `config path` prints the one in use.
`RECOIL_PROJECT` aims the project scope when the working directory can't;
`RECOIL_VERBOSE` turns on diagnostics.

### Session evidence is opt-in

Installed hooks can capture compact evidence from agent sessions, but nothing
is ingested until `config set session-evidence.enabled true`.
`session-evidence discover` lists other agents' sessions bound to the
repository without writing, `backfill` ingests them, and `forget` purges one
session's evidence with the memories mined from it.

### Automation proposes, agents and people author

Mining, sharing and refresh never invent memory. Indexed docs are `source`
memories with `validity: unknown`, imported peer memory is remote evidence, and
`wake` can mark a memory stale when its file changed but never rewrites it.
