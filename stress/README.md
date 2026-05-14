# Stress test harness

Non-benchmark stress testing of recoil against real repositories.

## Layout

- `manifest.json` — repo list with pinned SHAs, applied config, ground-truth answer paths.
- `queries/universal.json` — query archetypes applied to every repo.
- `queries/<repo-key>.json` — repo-specific queries layered on top.
- `configs/` — per-Phase-3 config variants (classify.override, rank, include/exclude).
- `synth/` — transcript synthesizer for Phase 2b.
- `run.sh` — end-to-end driver per repo.
- `aggregate.py` — produces the matrix view from per-repo reports.
- `reports/` — per-repo and aggregated JSON outputs.

## Clones

Clones live OUTSIDE this repo at `~/recoil-stress-clones/<repo-key>/<sha>/`. Downloaded
via GitHub tarball API (faster than `git clone` for our purposes, no .git overhead).
