# Contributing to recoil

Thanks for contributing to recoil. Bug fixes, documentation, and improvements
that keep the write path authored rather than automated are welcome.

## Building

recoil links SQLite with FTS5 through CGO, so a C compiler is required and
`CGO_ENABLED=0` builds do not work. The Makefile sets the flags:

```sh
make build        # ./recoil
make lint         # golangci-lint, config in .golangci.yml
make test         # go test ./... with FTS5 enabled
make ci           # build-check, lint, test, govulncheck
```

A bare `go test ./...` without `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5` fails with
`no such module: fts5`; use `make test`, or export the flag once.

The pre-commit hook in `.githooks/` runs gofmt, go vet, golangci-lint, and the
tests. Enable it with:

```sh
git config core.hooksPath .githooks
```

## Tests and your real store

The `cmd` suite redirects `HOME` and the XDG and AppData directories to a
throwaway directory in `TestMain`, and unsets `RECOIL_DB` and `RECOIL_PROJECT`,
so a test run can never open or mine into the store you use day to day. Keep
it that way: a test that needs a particular home sets its own with
`t.Setenv`, and a test that needs a store opens one under `t.TempDir()`.

## Submitting a pull request

Keep changes focused and add tests for behavior changes. Preserve the output
contracts agents depend on: the `{version, kind, data}` JSON envelope,
frontmatter-plus-content text output, `--minimal` rows, and the exit codes
documented in `MANUAL.md`. Explain any breaking change or new dependency.

Prefer the standard library; config is JSON under `os.UserConfigDir()`. Use
the Go version in [go.mod](go.mod), and run `make lint` and `make test` before
opening the PR.

If a change alters a command's behavior, update `MANUAL.md` (it is what
[recoil.sh](https://recoil.sh) renders) and add a line under `[Unreleased]` in
`CHANGELOG.md`.

## Reporting a problem

Include the command, expected and actual behavior, and `recoil version`. For
store problems, `recoil status` and `recoil swarm --json` describe the tree
without printing memory content.
