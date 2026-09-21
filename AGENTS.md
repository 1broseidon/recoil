# recoil — Architecture

Local-first memory for coding agents. One SQLite FTS5 store per machine holds
verbatim memories with provenance; agents `wake` at the start of a session,
`check` before acting against a remembered decision, and `handoff` at the end.
The manual for using it is [MANUAL.md](MANUAL.md); this file is the map of the
code for anyone changing it.

## Module Layout

```
main.go                      Thin entry point → cmd.Execute(); the exit code comes from cmd.HandleError
cmd/                         Cobra commands, one file per command, plus the helpers they share
  root.go                    Root command, global flags (--db, --json), help groups, store open helpers
  errors.go                  Exit-code classification (2 validation, 3 not found, 4 upstream, 5 precondition, 6 cancelled) and the JSON error envelope
  output.go                  {version, kind, data} JSON envelope, frontmatter-plus-content text, --minimal rows
  scope.go                   Scope flags and env aiming: --user, --project, --session, RECOIL_PROJECT, cwd
  setup.go / init.go         Bootstrap: .recoil/project.json marker, docs mining, hook install, sharing posture
  wake.go                    Bounded starter context in lanes (current decisions, project docs, recent evidence)
  search.go / retriever.go   FTS search, optional hybrid retrieval (FTS + embeddings, RRF fusion), --explain
  check.go                   Audit a proposed action against claim families: use / review / use_replacement
  list.go / show.go / claims.go / export.go / profile.go   The rest of the read side
  remember.go / add.go       Write a memory; remember infers the role and claim key from the text
  decide.go                  Decision with a required claim key; supersedes the family's previous decision
  handoff.go                 Structured handoff under handoff.latest, superseding the previous one
  supersede.go / mark.go / forget.go   Lifecycle: replace, change validity or links, tombstone or destroy
  auto_supersede.go / related_claims.go / decision_*.go   Claim-key families, contradiction checks, predicates
  mine.go / source_refresh.go          Project file ingestion and hash-tracked refresh of indexed docs
  session_evidence.go / session_backfill.go   Opt-in capture of selected, redacted slices of agent sessions
  hook.go                    Agent hooks: install / uninstall / remind for claude-code, codex, opencode
  mcp.go                     MCP stdio server (official go-sdk) exposing the recoil_* tools
  instructions.go            instruct <agent>: the short contract an agent's instructions file carries
  channel*.go / relay*.go / swarm.go   Sharing: channel subscriptions and auto-publish policy, the relay server and its admin, tree health
  embed.go / embedding_provider.go     Optional embedding sidecars (local, Ollama, OpenRouter)
  eval.go                    Offline retrieval eval over fixtures; the ranking is pinned by these
  tray.go                    System tray companion (fyne.io/systray)
  config.go / status.go / backup.go / repair.go / migrate.go / version.go   Operator commands
internal/
  store/                     SQLite: schema and migrations (PRAGMA user_version = SchemaVersion), memories / sources / memory_embeddings tables, the memories_fts FTS5 index, channel tables; OpenReadOnly refuses to migrate
  scope/                     Scopes: user, project (marker file, git root, worktree-aware so a worktree maps to its main clone's project), session
  config/                    Store and state paths (os.UserConfigDir, RECOIL_DB) and the typed settings registry behind `recoil config`
  retrieval/                 Query expansion and retrieval signals; personal-domain expansions are behind a default-off flag
  sourcequality/             Classifies source documents (agent instructions, contributing, security, …) and scores their prior
  mine/                      Discovers and chunks project files; honours .recoilignore
  pathmatch/                 gitignore-style pattern matching used by the miner and settings
  redact/                    Strips private keys, bearer tokens, API keys and secret-looking env assignments before anything is stored
  sessionevidence/           Selects load-bearing turns from a session payload and persists them as evidence memories
  agentsessions/             Finds and reads on-disk Claude Code and Codex transcripts for backfill
  channel/                   Signed channel artifacts, manifests and roster cards exchanged through a relay
  embedding/                 Embedding providers: local hashing, Ollama, OpenRouter
  eval/                      Fixture loading and retrieval metrics
  traystats/ trayautostart/  Tray companion: store statistics and login-item registration per OS
bench/                       Retrieval benchmarks (BEAM, LoCoMo, LongMemEval); a program under this module, not a test
stress/                      Stress harness over synthetic repos; also a program, not a test
eval/                        Eval fixtures and synthetic corpora; .recoilignore keeps the miner out of eval/corpora
scripts/                     CLI accuracy smoke checks that build the binary and drive it end to end
site/                        recoil.sh, printed from MANUAL.md by inkcap (GitHub Pages, .github/workflows/docs.yml)
```

`internal/` is deliberately internal: recoil is a CLI, not a library, and the
store schema is free to change behind `SchemaVersion`.

## Design Principles

- **Authorship, not automation.** Durable memory is authored by an agent or
  operator action (`remember`, `decide`, `handoff`, `mine` over files you
  control). Automation may enrich, connect, publish and review it, and may
  propose new memory, but proposals go to Review unless explicitly accepted.
  No LLM sits in the required write path.
- **Sourced answers.** Every retrieved memory carries its source agent, source
  path and ref, file hash and validity, so an agent can say where a claim came
  from and whether it still holds.
- **Lifecycle, not deletion.** A new decision in a claim family supersedes the
  old one and links both ways; retrieval demotes stale guidance rather than
  burying it. `forget` tombstones by default and destroys only on request.
- **Bounded by default.** `wake` prints a fixed character budget in lanes;
  `search` and `list` are limited; `export` is the one command that prints a
  family whole, on purpose.
- **Deterministic retrieval.** FTS5 ranking with documented boosts and
  penalties, pinned by the eval fixtures. Embeddings are an opt-in sidecar and
  hybrid mode falls back to FTS when the provider is unavailable.
- **Local and standalone.** One store under `os.UserConfigDir()/recoil`, a
  project marker in `.recoil/project.json`. Sharing between trees is opt-in,
  moves only signed artifacts through a relay, and never invents memory.
- **Agent-readable output.** Text output is frontmatter plus content; `--json`
  is a stable `{version, kind, data}` envelope; `--minimal` is TSV; errors
  carry a code and map to documented exit statuses.

## MCP Server

`recoil mcp` runs an MCP server over stdio through the official go-sdk. The
tools mirror the CLI and call the same functions: `recoil_search`,
`recoil_wake`, `recoil_check`, `recoil_list`, `recoil_claims`, `recoil_export`
on the read side, `recoil_add`, `recoil_remember`, `recoil_handoff` on the
write side. Operator commands (`setup`, `config`, `relay`, `forget --destroy`)
are deliberately not tools. Errors map to the same codes as the CLI.

## Quality Standards

- `make lint` (golangci-lint, config in `.golangci.yml`) and `make test` must
  pass. The gocyclo ceiling is set where nothing trips it today and only moves
  down.
- `CGO_ENABLED=1` with `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5`. The Makefile sets
  both; a bare `go test ./...` fails with `no such module: fts5`.
- The `cmd` suite's `TestMain` isolates `HOME`, the XDG and AppData
  directories, `RECOIL_DB` and `RECOIL_PROJECT`. Never write a test that
  reaches the developer's real store.
- `.githooks/pre-commit` runs gofmt, vet, lint and the suite; enable it with
  `git config core.hooksPath .githooks`.
- Standard library first. Config is JSON under `os.UserConfigDir()`; no
  config libraries, no assertion libraries in tests.

## CLI Usage

```
recoil setup                                       # mark the project, index docs, install hooks
recoil wake --max-chars 1600                       # bounded starter context, current decisions first
recoil search "why did we change auth"             # FTS over the project scope
recoil check "add Redis for this cache"            # use / review / use_replacement against claim families
recoil remember "Auth tokens live in the keyring" --agent codex
recoil decide --claim-key auth.token-storage "Auth tokens live in the keyring"
recoil supersede <id> "Replacement text"           # correct a memory that is now wrong
recoil handoff --agent codex --next-step "Verify the token migration"
recoil claims                                      # claim families in scope
recoil export --claim-key-prefix dependency.       # a family verbatim, for prompt injection
recoil list --role adr --validity active
recoil show <id>
recoil mine --dry-run                              # what the miner would index
recoil swarm                                       # the tree and its sharing health
recoil instruct claude-code                        # the contract for an instructions file
recoil hook install claude-code                    # keep an agent on the contract
recoil mcp                                         # the same tools over stdio
```

## Flags

Global: `-d/--db <path>` (or `RECOIL_DB`) aims at another store; `--json` selects
the envelope. Memory commands take `--user`, `--project <path>` and
`--session <id>` for scope, `RECOIL_PROJECT` aims the project from the
environment, and `RECOIL_VERBOSE` turns on the routine warnings that stay
quiet otherwise. The filter flags (`--current`, `--historical`,
`--validity`, `--role`, `--agent`, `--claim-key`, `--since`, `--before`,
`--source`, `--source-kind`) are shared by `wake`, `search`, `list` and
`forget`.

## Adding a command

1. One file in `cmd/`, a `newXCommand()` constructor, registered in
   `cmd/root.go` under the help group it belongs to.
2. Open the store with `openReadStore` for reads, `openContextStore` for
   reads that may refresh from a channel, `openStore` only for writes.
3. Resolve scope through the shared scope flags; never read the cwd directly.
4. Text output through `frontmatter` and the `kv` helpers, JSON through
   `writeJSON(w, "<kind>_result", data)`. Return errors; `HandleError`
   classifies them, so error text that starts with the documented phrases
   ("not found", "invalid", "requires") lands on the right exit code.
5. Document it in `MANUAL.md` (the published manual) and add a line under
   `[Unreleased]` in `CHANGELOG.md`.
