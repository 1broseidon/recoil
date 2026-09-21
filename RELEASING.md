# Releasing recoil

Maintainer runbook. Contributors don't need any of this —
see [CONTRIBUTING.md](CONTRIBUTING.md).

Pushing a `v*` tag runs [`.github/workflows/release.yml`](.github/workflows/release.yml),
which does everything below. Cutting a release is:

```sh
# add a "## [X.Y.Z] - YYYY-MM-DD" section to CHANGELOG.md, then
git commit -m "chore(release): vX.Y.Z"
git push origin main
git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z
```

The version is the tag: nothing in the source is bumped. `make build` stamps
`git describe` into the binary; the workflow stamps the tag.

## What the workflow does

recoil links SQLite with FTS5 through CGO, so it cannot be cross-compiled the
way a pure-Go binary can. The `build` job runs once per target on a native
runner, tests there, and builds there:

| Target | Runner |
|---|---|
| `linux/x86_64` | `ubuntu-latest` |
| `linux/arm64` | `ubuntu-24.04-arm` |
| `darwin/arm64` | `macos-latest` |
| `darwin/x86_64` | `macos-latest`, cross-compiled with `-arch x86_64` |
| `windows/x86_64` | `windows-latest`, statically linked |

The `release` job renames the archives to `recoil_<tag>_<os>_<arch>.tar.gz`
(`.zip` on Windows), writes one `checksums.txt` over all of them, extracts the
tag's section from `CHANGELOG.md` as the release notes, and publishes the
GitHub release. **It fails without a matching `## [X.Y.Z]` section**, so write
the changelog before tagging.

The `docker` job builds `ghcr.io/1broseidon/recoil:<tag>` and `:latest` for
`linux/amd64` and `linux/arm64`. The image's default command is the relay
server (`recoil relay serve`), not the CLI.

## Distribution channels

| Channel | How it updates |
|---|---|
| GitHub release | The workflow, from the tag |
| `curl -fsSL https://recoil.sh/install \| sh` | Resolves `/releases/latest` at run time — nothing to publish |
| `irm https://recoil.sh/install.ps1 \| iex` | Same, for Windows |
| `ghcr.io/1broseidon/recoil` | The `docker` job |
| `go install github.com/1broseidon/recoil@latest` | The Go module proxy; needs `CGO_CFLAGS=-DSQLITE_ENABLE_FTS5` |
| chain.sh bootstrap | Reads the latest release by platform suffix — nothing to publish |
| recoil.sh | `docs.yml` reprints the manual on push to `main`; the version chip reads git tags |

## Rules learned the hard way

- **A tag never moves.** `v0.1.0` was tagged before its release build passed,
  the build failed on macOS, and the tag was already cached by
  `proxy.golang.org`. It stays as a tag with no release; `v0.1.1` is the first
  published one. If a release build fails, fix `main` and cut the next patch.
- **Re-running is by tag.** `gh workflow run release.yml -f tag=vX.Y.Z` builds
  an existing tag again, for instance after fixing the workflow itself on a
  branch: `--ref <branch>` picks the workflow, the sources still come from the
  tag.
- **A release created with `GITHUB_TOKEN` fires no other workflow.** The
  `release: published` trigger in `docs.yml` never sees it; the version chip on
  recoil.sh follows the `chore(release)` commit on `main` instead.
- **Test the candidate before tagging.** Install it locally, run it against
  the previous release's binary on a real store, and only then tag. The
  workflow runs the suite on every runner, but that is a gate, not a test of
  the release.
