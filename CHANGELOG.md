# Changelog

All notable changes to recoil are documented here.

## [Unreleased]

## [0.1.0] - 2026-09-19

First tagged release. Until now recoil was install-from-source only: `go install` with `CGO_ENABLED=1` and `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5`, which required a Go toolchain and a C compiler on every machine that wanted it. This release publishes prebuilt binaries so that stops being a prerequisite.

### Added

- Prebuilt binaries attached to each tagged release, with a `checksums.txt` covering every archive: `linux/x86_64`, `linux/arm64`, `darwin/arm64`, `darwin/x86_64`, and `windows/x86_64`.
- `install.ps1` and `uninstall.ps1` for Windows, installing to `%LOCALAPPDATA%\recoil` and adding it to the user `PATH`. `uninstall.ps1` keeps your memory database by default; `-Purge` removes it too.
- A release workflow that builds each target on a native runner, so the CGO build linking SQLite with FTS5 is compiled for the platform it ships to rather than cross-compiled.

### Notes

- FTS5 remains mandatory. It is compiled into every published binary, so the requirement no longer shows up as a build flag you have to remember — but a binary built without it still will not work, and there is no degraded fallback.
- Building from source is unchanged and still supported: `make build` and `make install` set the CGO flags for you.
