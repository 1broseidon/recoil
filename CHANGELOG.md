# Changelog

All notable changes to recoil are documented here.

## [Unreleased]

### Removed

- Memory sharing. The `swarm`, `channel` and `relay` commands, the relay server and its Docker image, the `--relay`, `--relay-agent`, `--standalone`, `--collaborative` and `--manual-share` flags on `setup`, the `--publish` / `--no-publish` flags on `add`, `remember`, `decide`, `supersede`, `handoff` and the MCP write tools, the seven `channel.*` settings, the Peer Memory lane, and the `channel_*`, `peer_memory_received`, `memory_shared`, `pending_share` and `publish` fields in text and JSON output. Recoil is one local store per machine; nothing in it talks to a network except the optional embedding providers.

### Changed

- Store schema version 2. The first command that opens an older store drops the channel tables it carries; memories, sources and embeddings are untouched.
- `recoil setup` prints the project, chunk and hook counts and nothing about posture or sharing.
- The agent contract (`recoil instruct`) describes a memory store, not a memory tree, and no longer promises automatic sharing.

## [0.2.0] - 2026-09-21

The repository now ships the way the other chain.sh tools do: CI on every push, lint and vulnerability gates, contributor docs, and a grouped `--help`. The MCP SDK and Go toolchain move past four SDK advisories and ten standard-library ones.

### Added

- A CI workflow: build, golangci-lint, the test suite on Linux and macOS, and govulncheck, on every pull request and push to `main`. `make lint`, `make vulncheck`, `make build-check` and `make ci` run the same checks locally, and `.githooks/pre-commit` runs them before a commit (`git config core.hooksPath .githooks`).
- `AGENTS.md` (code layout and conventions, with `CLAUDE.md` pointing at it), `CONTRIBUTING.md`, and `RELEASING.md`.
- `recoil --help` groups the commands the way the manual does: session, writing memory, reading memory, sources, sharing, for agents, operator.
- `recoil version` reports the module version and VCS stamp for a `go install github.com/1broseidon/recoil@vX.Y.Z` build instead of `dev (unknown, unknown)`.

### Changed

- `modelcontextprotocol/go-sdk` 1.2.0 → 1.8.0, which closes GO-2026-4569, GO-2026-4770, GO-2026-4773 and GO-2026-5771.
- Go toolchain floor raised from 1.26.2 to 1.26.7: govulncheck flagged ten standard-library findings against 1.26.2 (`net/http`, `crypto/tls`, `crypto/x509`, `net/url`, `net/textproto`, `encoding/asn1`, `net`), all fixed by 1.26.6. CI builds with `go-version-file: go.mod`; local builds with Go 1.21+ fetch the toolchain automatically.
- `recoil instructions` is a hidden alias of `recoil instruct`; the two commands already printed the same text.
- `ProjectScope` no longer resolves a subdirectory to itself when recoil runs inside a git hook: the `git` it spawns now ignores the `GIT_DIR`, `GIT_WORK_TREE` and `GIT_INDEX_FILE` a hook exports.
- The `cmd` test suite redirects `HOME` and the XDG and AppData directories to a throwaway directory and unsets `RECOIL_DB` and `RECOIL_PROJECT`, so a test run can no longer open or mine into the developer's real store.

## [0.1.1] - 2026-09-21

The first release with published binaries. v0.1.0 was tagged, but its release build failed on the macOS runner and never published anything.

### Added

- `install.sh`, served at `https://recoil.sh/install`: detects the OS and architecture, verifies the archive against `checksums.txt`, and installs `recoil` into `/usr/local/bin` or `~/.local/bin`. `install.ps1` and `uninstall.ps1` are served at `https://recoil.sh/install.ps1` and `https://recoil.sh/uninstall.ps1`.
- The manual at [recoil.sh](https://recoil.sh), printed from `MANUAL.md` by inkcap on GitHub Pages.

### Fixed

- Relay: a single-use invite that has already been claimed now answers `403 invite has already been used` instead of `404 invite not found`. The loser of two concurrent joins got one or the other depending on timing, which is also what made the release test suite fail on macOS.

## [0.1.0] - 2026-09-19

First tagged release. Until now recoil was install-from-source only: `go install` with `CGO_ENABLED=1` and `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5`, which required a Go toolchain and a C compiler on every machine that wanted it. This release publishes prebuilt binaries so that stops being a prerequisite.

### Added

- Prebuilt binaries attached to each tagged release, with a `checksums.txt` covering every archive: `linux/x86_64`, `linux/arm64`, `darwin/arm64`, `darwin/x86_64`, and `windows/x86_64`.
- `install.ps1` and `uninstall.ps1` for Windows, installing to `%LOCALAPPDATA%\recoil` and adding it to the user `PATH`. `uninstall.ps1` keeps your memory database by default; `-Purge` removes it too.
- A release workflow that builds each target on a native runner, so the CGO build linking SQLite with FTS5 is compiled for the platform it ships to rather than cross-compiled.

### Notes

- FTS5 remains mandatory. It is compiled into every published binary, so the requirement no longer shows up as a build flag you have to remember — but a binary built without it still will not work, and there is no degraded fallback.
- Building from source is unchanged and still supported: `make build` and `make install` set the CGO flags for you.
