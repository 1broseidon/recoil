#!/usr/bin/env bash
# Download BEAM (Beyond a Million Tokens, ICLR 2026) conversations + probing
# questions into bench/.corpus/beam/<scale>/<conv>/. We only fetch chat.json
# and probing_questions/probing_questions.json per conversation — pickles,
# plans, labels, etc. are not needed for retrieval scoring.
set -euo pipefail

scale="${1:-100K}"
case "$scale" in
  100K|500K|1M|10M) ;;
  *) echo "scale must be one of 100K, 500K, 1M, 10M (got $scale)"; exit 2;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="$repo_root/bench/.corpus/beam/$scale"
mkdir -p "$dest"

api="https://api.github.com/repos/mohammadtavakoli78/BEAM/contents/chats/$scale"
raw="https://raw.githubusercontent.com/mohammadtavakoli78/BEAM/main/chats/$scale"

# List conversation dirs.
convs=$(curl -sfL "$api" | jq -r '.[] | select(.type=="dir") | .name' | sort -n)
count=$(echo "$convs" | wc -l)
echo "[beam] scale=$scale conversations=$count dest=$dest"

i=0
for c in $convs; do
  i=$((i+1))
  out="$dest/$c"
  mkdir -p "$out/probing_questions"
  if [ -f "$out/chat.json" ] && [ -f "$out/probing_questions/probing_questions.json" ]; then
    echo "[skip] $scale/$c"
    continue
  fi
  echo "[fetch $i/$count] $scale/$c"
  curl -sfL "$raw/$c/chat.json" -o "$out/chat.json"
  curl -sfL "$raw/$c/probing_questions/probing_questions.json" -o "$out/probing_questions/probing_questions.json"
done

echo "[done] $dest"
du -sh "$dest"
