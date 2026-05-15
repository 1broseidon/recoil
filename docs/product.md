# Recoil Product Notes

Recoil is a fast local operational memory CLI for long-running agentic
workflows. Coding agents are the first wedge, but the product should work for
any bounded project where durable local evidence is cheaper than guessing.

Its product thesis is simple:

> Make the right local evidence cheaper to retrieve than guessing.

The practical-memory P-series is documented in `docs/P_SERIES.md`. The board
is the source of truth for day-to-day work. Use Brainfile for sequencing:

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
The same loop should also prove out on research, writing, customer/account,
planning, and other non-coding projects with durable decisions and preferences.

The minimum loop:

1. Operator runs `recoil setup`.
2. Agent starts with `recoil wake`.
3. Agent searches before relying on prior project context.
4. Agent stores durable context during work with `recoil remember`.
5. Agent closes the session with `recoil handoff`.
6. Operator can inspect and correct memory with `list`, `show`, and `forget`.

If that loop is fast, sourced, and boring, richer retrieval and sync have a
foundation.

## Core Commands

```sh
recoil setup
recoil setup --relay http://localhost:8787/v1/invites/<token>
recoil setup --manual-share --relay http://localhost:8787/v1/invites/<token>
recoil swarm
recoil instruct <agent>
recoil init
recoil wake --max-chars 1600
recoil search "<topic>"
recoil search "<topic>" --profiles auto
recoil remember --agent <agent> "<durable memory>"
recoil handoff --agent <agent> --next-step "<next action>"
recoil add --agent <agent> --role decision "<durable memory>"
recoil mine
recoil profile --entity "<name>"
recoil eval
recoil eval --suite workflows
recoil eval --suite workflows --out eval/results
recoil mcp
recoil relay serve --addr :8787 --data /data
recoil relay invite --data /data --channel agents --relay-url http://localhost:8787
recoil list
recoil show <memory-id>
recoil mark <memory-id> --validity stale
recoil supersede <old-memory-id> "<replacement memory>"
recoil forget <memory-id>
```

`setup` is the operator bootstrap command: initialize, first mine, auto-detect
installed agents and install hooks, optionally join sharing, then print
`recoil wake` as the next command. `--relay` defaults to collaborative posture;
`--manual-share --relay` receives peer memory but leaves automatic sharing off;
`--standalone` keeps the memory tree local only.

`swarm` is the daily operator health card: tree, posture, sharing, peers,
memory, peer memory, evidence, review, and pending share. `instruct` prints the
short agent contract for `wake`, `remember`, and `handoff`.

`wake` is the session boot command. It refreshes changed tracked project files
just-in-time, then orients an agent with grouped, sourced context rather than
behave like an unstructured recent-memory dump.

`remember` is the default write verb. It infers role and claim key
deterministically, with `add` and `decide` remaining as explicit power-user
paths. `handoff` is the session-end pair to `wake`, capturing decisions made,
constraints discovered, next steps, supersessions, and open questions.

Inside an initialized project, no scope flag means the current project. Use
`--user` for durable cross-project preferences and `--session <id>` for
one-session memories.

## Settled Decisions

Reference these Brainfile records for build sequencing, and promote durable
product truths into ordinary docs or direct Recoil memories:

- `decision-2`: Product thesis, recall must be cheaper than guessing.
- `adr-1`: Local SQLite FTS5 with deterministic writes.
- `decision-3`: CLI default is current initialized project scope.
- `decision-4`: Hooks and skills before MCP; MCP is now a thin read-only
  stdio bridge after CLI semantics stabilized.
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
11. `p0-workflows`: Prove coding and non-coding continuity through
    `recoil eval --suite workflows`. (done)
12. `p0-profiles`: Add deterministic entity profiles with an exact-detail
    opt-out router in search. (done)
13. `p2-mcp`: Add a minimal read-only MCP stdio server using the official Go
    SDK, with writes gated by `--allow-write`. (done)
14. `p1-retriever`: Share retrieval plumbing across search, eval, MCP, hook,
    wake, and profile paths. (done)
15. `p1-adapters`: Add adapter payload fixtures for Claude Code, Codex, and
    OpenCode session evidence. (done)
16. `p1-reports`: Add normalized JSON and Markdown eval artifacts via
    `recoil eval --out`. (done)
17. `p2.5-decision-relevance`: Add optional decision predicates,
    `recoil check`, and `wake --include-decisions` so agents can push back on
    stale or rejected decisions with sourced receipts. (done)
18. `p3-decision-verdict-maturity`: Add date predicate capture hygiene,
    decision stance/opposition verdicts, narrow Tier 1 evaluators, and
    Tier 1 / Tier 2 advisory composition. (done)

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

## Distributed Memory Direction

Distributed memory is not global memory. On one machine, Recoil should stay
fully local: project/user/session scopes and local search are enough for cross
or adjacent project memory. The network path exists for machine-to-machine
exchange.

Current product decision: a workspace has a memory tree. A relay lets another
tree receive selected peer memory over a durable signed replay log; each node
keeps its own local memory projection. The relay is a dumb pipe, not a memory
authority.

The current experimental path is:

- `recoil setup --relay <invite-url>` joins sharing in collaborative posture:
  automatic eligible-memory sharing, context-time peer-memory refresh, and
  session evidence enabled with redaction.
- `recoil setup --manual-share --relay <invite-url>` receives peer memory but
  keeps automatic sharing off.
- `recoil swarm` summarizes tree health without exposing transport internals;
  `recoil swarm --refresh` sends pending shares and receives peer memory.
- `recoil instruct <agent>` generates the short agent contract: `wake` at
  start, `remember` when durable intent is explicit, `handoff` at the end.
- `recoil relay serve` runs a self-hosted Docker-friendly relay backed by a
  data directory.
- `recoil relay invite` creates a one-time no-password registration invite.
- `recoil wake`, `search`, `check`, and `handoff` receive peer memory with a
  short fail-soft timeout before returning context or closing out work.
- `recoil channel refresh`, `channel publish --claim-key/--id/--since
  --dry-run`, `channel outbox`, and `channel roster` remain lower-level admin
  surfaces for operators who need transport precision.
- `channel.auto_publish=off|guidance|all-local` can share eligible memory from
  write verbs automatically. Guidance mode shares claim-keyed durable guidance;
  failed shares remain pending locally.
- Peer memory imports into the local scope with `source_kind: remote_artifact`
  and provenance metadata for compatibility, while user-facing output says
  Peer Memory and `shared by <agent>`.
- `recoil relay status`, `relay invite list/status/revoke`, `relay member
  list/kick`, `relay channel create/inspect`, `relay setup`, and `relay doctor`
  expose the relay lifecycle without filesystem surgery.

MCP remains a local query/access bridge. The relay is the distribution plane. A
future Cloudflare Worker should host the same signed log API without becoming
the source of truth.

Access on the relay is keyed by the node's ed25519 roster card, not by the
invite. Invites are strictly one-time bootstraps; once a roster card is on the
relay, the node can publish, refresh, sync, and read by signing requests with
its private key until an operator kicks the member or the roster card ages out.
Open gaps to address before the relay is more than V0:

- No key rotation. A node's keypair is generated once per local database; if
  the private key leaks, the operator-side mitigation is member kick plus
  re-join under a new identity.
- TLS is still expected at the reverse proxy or tunnel layer. The relay warns
  on plain HTTP but does not terminate TLS itself.

## Stale Memory Rule

Old evidence is not automatically bad. It becomes dangerous when it looks like
current truth.

Recoil should preserve rejected and superseded history, but retrieval must make
current guidance unmistakable. In practice:

- memories carry `validity`, `claim_key`, `supersedes`, and `superseded_by`
  fields in the store.
- `wake` excludes rejected, superseded, stale, historical, and tombstoned
  memories by default, and self-refreshes changed tracked file chunks before
  rendering.
- `search` and `wake` group output into Current Decisions, Peer Memory,
  Project Docs, Recent Evidence, and Historical lanes, with a one-line `why`
  explanation per result.
- evals must include stale/superseded cases before ranking is tuned.

## Non-Goals For The Core Path

- No hosted service in the core local memory path.
- No daemon in the core local memory path.
- No cloud sync in the core local memory path.
- No LLM in the write path.
- No inferred knowledge graph before verbatim recall is solid.
