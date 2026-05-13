#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin_dir="$(mktemp -d)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir" "$work_dir"' EXIT

bin="$bin_dir/recoil"
db="$work_dir/recoil.db"
transcript="$work_dir/transcript.json"

CGO_CFLAGS='-DSQLITE_ENABLE_FTS5' go build -o "$bin" "$repo_root"

cd "$work_dir"
cat > "$transcript" <<'JSON'
{
  "session_id": "sess-cli-accuracy",
  "turns": [
    {"turn_index": 1, "role": "user", "content": "I prefer deep learning for medical image analysis and I enjoy recent healthcare AI papers."},
    {"turn_index": 2, "role": "assistant", "content": "I will keep that in mind for future recommendations."},
    {"turn_index": 3, "role": "user", "content": "Switch to mattn/go-sqlite3 now; we no longer use modernc because FTS5 support is required."},
    {"turn_index": 4, "role": "assistant", "content": "Understood. I will treat mattn/go-sqlite3 as the current SQLite driver."},
    {"turn_index": 5, "role": "user", "content": "I had a follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy."},
    {"turn_index": 6, "role": "assistant", "content": "That is useful health context."},
    {"turn_index": 7, "role": "user", "content": "Dr. Smith prescribed antibiotics after the urinary tract infection."},
    {"turn_index": 8, "role": "assistant", "content": "I will remember the separate doctor context."},
    {"turn_index": 9, "role": "user", "content": "I have 3 sisters and one brother, and my weekly book club has 10 women, 4 men, and 1 non-binary member."},
    {"turn_index": 10, "role": "assistant", "content": "I will remember that family and book club context."},
    {"turn_index": 11, "role": "user", "content": "I bought a smoker today for BBQ experiments with hickory and apple wood."},
    {"turn_index": 12, "role": "assistant", "content": "Nice, that smoker context may matter for future BBQ questions."}
  ]
}
JSON

"$bin" -d "$db" init >/dev/null
"$bin" -d "$db" add --role decision --claim-key dependency.sqlite-driver \
  "Current sqlite driver is mattn/go-sqlite3 with FTS5 enabled." >/dev/null
"$bin" -d "$db" add --role decision --validity rejected --claim-key dependency.sqlite-driver \
  "Rejected modernc sqlite driver because FTS5 support is required." >/dev/null
"$bin" -d "$db" add --role constraint \
  "Never store API keys or bearer tokens in plaintext project docs." >/dev/null
"$bin" -d "$db" session-evidence ingest --file "$transcript" --force --agent codex --session-id sess-cli-accuracy >/dev/null

python3 - "$bin" "$db" <<'PY'
import json
import subprocess
import sys

bin_path, db = sys.argv[1], sys.argv[2]

cases = [
    {
        "id": "direct-current-driver",
        "command": ["search", "--limit", "3", "current sqlite driver FTS5"],
        "must": ["mattn/go-sqlite3"],
        "must_not": ["Rejected modernc"],
    },
    {
        "id": "plain-secret-constraint",
        "command": ["search", "--limit", "3", "where should bearer tokens be stored"],
        "must": ["Never store API keys"],
    },
    {
        "id": "profile-interest",
        "command": ["search", "--limit", "3", "publications conferences medical imaging"],
        "must": ["Derived user profile trace", "medical image"],
    },
    {
        "id": "update-trace",
        "command": ["search", "--limit", "3", "current SQLite driver modernc no longer"],
        "must": ["Derived update trace", "Switch to mattn/go-sqlite3"],
    },
    {
        "id": "doctor-two-facts",
        "command": ["search", "--limit", "5", "Which doctor discussed biopsy and which doctor handled the UTI?"],
        "must": ["Dr. Lee", "Dr. Smith"],
    },
    {
        "id": "siblings-count-context",
        "command": ["search", "--limit", "5", "total number of siblings brother sisters"],
        "must": ["3 sisters", "one brother"],
    },
    {
        "id": "book-club-demographics",
        "command": ["search", "--limit", "5", "book club 10 women 4 men non-binary"],
        "must": ["10 women", "4 men", "non-binary"],
    },
    {
        "id": "purchase-appliance",
        "command": ["search", "--limit", "5", "kitchen appliance bought BBQ hickory apple"],
        "must": ["smoker", "hickory", "apple wood"],
    },
    {
        "id": "role-filter-decision",
        "command": ["search", "--role", "decision", "--limit", "3", "sqlite driver"],
        "must": ["mattn/go-sqlite3"],
    },
    {
        "id": "wake-query-context",
        "command": ["wake", "--limit", "5", "medical imaging publications"],
        "must": ["Derived user profile trace", "medical image"],
    },
]

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
    return []

results = []
for case in cases:
    try:
        rows = run_json(case["command"])
        text = "\n".join(
            " ".join(
                str(row.get(key, ""))
                for key in ("content", "role", "source_ref", "source_path")
            )
            for row in rows
        ).lower()
        must = case.get("must", [])
        must_not = case.get("must_not", [])
        passed = all(item.lower() in text for item in must) and all(item.lower() not in text for item in must_not)
        results.append(
            {
                "id": case["id"],
                "passed": passed,
                "count": len(rows),
                "must": must,
                "must_not": must_not,
                "top_roles": [row.get("role", "") for row in rows[:3]],
                "top_excerpt": [row.get("content", "")[:140] for row in rows[:3]],
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
    "commands": ["init", "add", "session-evidence ingest", "search --json", "wake --json"],
    "results": results,
}
print(json.dumps(summary, indent=2))
if passed != len(results):
    sys.exit(1)
PY
