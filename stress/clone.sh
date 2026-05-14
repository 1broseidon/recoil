#!/usr/bin/env bash
# Tarball-download repos pinned in manifest.json to ~/recoil-stress-clones/<key>/<ref>/
# Idempotent: skips repos already on disk.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$repo_root/stress/manifest.json"
clones_root="${RECOIL_CLONES_ROOT:-$HOME/recoil-stress-clones}"
mkdir -p "$clones_root"

count=$(jq '.repos | length' "$manifest")
i=0
while [ "$i" -lt "$count" ]; do
  key=$(jq -r ".repos[$i].key" "$manifest")
  url=$(jq -r ".repos[$i].url" "$manifest")
  ref=$(jq -r ".repos[$i].ref" "$manifest")
  dest="$clones_root/$key/$ref"
  if [ -d "$dest" ] && [ -n "$(ls -A "$dest" 2>/dev/null)" ]; then
    echo "[skip]   $key@$ref already at $dest"
    i=$((i+1))
    continue
  fi
  echo "[fetch]  $key@$ref"
  mkdir -p "$dest"
  owner_repo="${url#https://github.com/}"
  tarball="https://codeload.github.com/$owner_repo/tar.gz/refs/tags/$ref"
  # Try tag first, then branch
  if ! curl -sfL --max-time 600 "$tarball" -o "/tmp/$key.tar.gz"; then
    tarball="https://codeload.github.com/$owner_repo/tar.gz/refs/heads/$ref"
    if ! curl -sfL --max-time 600 "$tarball" -o "/tmp/$key.tar.gz"; then
      tarball="https://codeload.github.com/$owner_repo/tar.gz/$ref"
      curl -sfL --max-time 600 "$tarball" -o "/tmp/$key.tar.gz"
    fi
  fi
  tar -xzf "/tmp/$key.tar.gz" -C "$dest" --strip-components=1
  rm -f "/tmp/$key.tar.gz"
  echo "[done]   $key -> $dest ($(du -sh "$dest" | cut -f1))"
  i=$((i+1))
done

echo "all clones in $clones_root"
du -sh "$clones_root"/*/* 2>/dev/null || true
