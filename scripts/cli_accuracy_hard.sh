#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_dir="$repo_root/cli-fixtures/hard"
cases_file="$fixture_dir/cases/search_cases.json"
bin_dir="$(mktemp -d)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir" "$work_dir"' EXIT

bin="$bin_dir/recoil"
db="$work_dir/recoil.db"

CGO_CFLAGS='-DSQLITE_ENABLE_FTS5' go build -o "$bin" "$repo_root"

"$bin" -d "$db" init >/dev/null

"$bin" -d "$db" add --role decision --claim-key dependency.sqlite-driver \
  "Current sqlite driver is mattn/go-sqlite3 with FTS5 enabled." >/dev/null
"$bin" -d "$db" add --role decision --validity rejected --claim-key dependency.sqlite-driver \
  "Rejected modernc sqlite driver because FTS5 support was not good enough for the current product path." >/dev/null
"$bin" -d "$db" add --role decision --claim-key architecture.local-first \
  "Recoil is local-first and should not require a hosted service or background daemon in v0." >/dev/null
"$bin" -d "$db" add --role decision --claim-key session-evidence.min-chars \
  "Current session evidence min-chars default is 200 because 120 captured too much routine chatter." >/dev/null
"$bin" -d "$db" add --role note --claim-key cli.accuracy.gate \
  "CLI accuracy gate uses real product commands: init, add, mine, session-evidence ingest, search --json, and wake --json." >/dev/null

"$bin" -d "$db" mine --agent hard-fixture --role source "$fixture_dir/docs" >/dev/null
for transcript in "$fixture_dir"/transcripts/*.json; do
  session_id="$(basename "$transcript" .json)"
  "$bin" -d "$db" session-evidence ingest --file "$transcript" --force --agent codex --session-id "$session_id" >/dev/null
done

python3 - "$bin" "$db" "$fixture_dir" "$cases_file" <<'PY'
import json
import subprocess
import sys

bin_path, db, fixture_dir, cases_file = sys.argv[1:5]

with open(cases_file, "r", encoding="utf-8") as fh:
    cases = json.load(fh)


def run_json(command):
    proc = subprocess.run(
        [bin_path, "-d", db, "--json", *command],
        text=True,
        capture_output=True,
    )
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip() or proc.stdout.strip())
    payload = json.loads(proc.stdout)
    data = payload.get("data", payload)
    if isinstance(data, list):
        return data
    if isinstance(data, dict) and "results" in data:
        return data["results"]
    if isinstance(data, dict) and "layers" in data:
        rows = []
        for layer in data["layers"]:
            rows.extend(layer.get("memories", []))
        return rows
    return []


def row_text(row):
    fields = (
        "content",
        "role",
        "source_kind",
        "source_agent",
        "source_path",
        "source_ref",
        "metadata",
        "claim_key",
        "validity",
    )
    return " ".join(str(row.get(key, "")) for key in fields)


results = []
for case in cases:
    try:
        rows = run_json(case["command"])
        text = "\n".join(row_text(row) for row in rows).lower()
        must = case.get("must", [])
        must_not = case.get("must_not", [])
        missing = [item for item in must if item.lower() not in text]
        forbidden = [item for item in must_not if item.lower() in text]
        empty_failure = bool(case.get("expect_empty")) and len(rows) != 0
        passed = not missing and not forbidden and not empty_failure
        results.append(
            {
                "id": case["id"],
                "passed": passed,
                "count": len(rows),
                "missing": missing,
                "forbidden": forbidden,
                "expected_empty_but_returned": empty_failure,
                "top_roles": [row.get("role", "") for row in rows[:5]],
                "top_sources": [row.get("source_path", "") for row in rows[:5]],
                "top_excerpt": [row.get("content", "")[:220] for row in rows[:5]],
            }
        )
    except Exception as exc:
        results.append({"id": case["id"], "passed": False, "error": str(exc)})

passed = sum(1 for result in results if result["passed"])
summary = {
    "cases": len(results),
    "passed": passed,
    "failed": len(results) - passed,
    "accuracy": passed / len(results) if results else 0,
    "fixture_dir": fixture_dir,
    "commands": [
        "init",
        "add",
        "mine",
        "session-evidence ingest",
        "search --json",
        "wake --json",
    ],
    "results": results,
}
print(json.dumps(summary, indent=2))
if passed != len(results):
    sys.exit(1)
PY
