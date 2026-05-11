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

recoil search "why did we change auth?"
recoil wake --max-chars 1600
recoil mine --dry-run
recoil mine

recoil show <memory-id>
recoil list --since 7d
recoil forget <memory-id>
recoil forget <memory-id> --destroy

recoil instructions codex
recoil hook remind
recoil repair
```

Default output is frontmatter plus content. Use `--json` for a stable
versioned envelope and `--minimal` on scan commands for tab-separated rows.

Detailed product notes have been consolidated into Brainfile records under
`.brainfile/board/`; `docs/product.md` is the compact index.

## Mining Project Files

`recoil mine` imports conservative markdown/text project files as sourced
memory chunks. It skips hidden and tooling directories such as `.git`,
`.recoil`, `.brainfile`, `node_modules`, and `vendor` by default.

```sh
recoil mine --dry-run
recoil mine docs/
```

Mined memories keep `source_path` and `source_ref` line ranges so agents can
show where local evidence came from.

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

## V0 Non-Goals

- No cloud sync.
- No daemon.
- No hosted dashboard.
- No MCP server before the CLI is excellent.
- No embeddings in the required path.
- No LLM-based extraction in the default write path.
- No inferred room/topic hard filters.
- No automatic rewriting of older memories.
