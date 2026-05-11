# Recoil Eval Fixtures

`fixtures.jsonl` defines the first retrieval eval corpus for Recoil. It is
intended for `task-2`, where a harness can seed a temporary database, run
`search` and `wake`, and report retrieval quality before ranking or stale-memory
behavior is tuned.

The file is JSON Lines. Each line is either a seeded memory or an eval case.

Run the harness with:

```sh
recoil eval
recoil eval eval/fixtures.jsonl
```

The command uses an isolated temporary database by default so eval fixtures do
not pollute the operator's real memory store.

## Memory Lines

Memory records have `type: "memory"` and a fixture-local `id`:

```json
{"type":"memory","id":"mem_sqlite_mattn","scope":{"kind":"project","id":"project-recoil"},"role":"decision","content":"...","metadata":{"validity":"active","claim_key":"dependency.sqlite-driver"}}
```

The `id` is not a Recoil public memory ID. Recoil IDs are generated from the
write payload, so the eval harness should map inserted Recoil IDs back to these
fixture IDs after seeding.

Memory fields:

- `scope.kind`: `project`, `user`, or `session`.
- `scope.id`: fixture-local scope ID.
- `role`, `source_agent`, `source_path`, and `source_ref`: seed metadata to pass
  through to Recoil when supported. Canonical source paths should model ordinary
  project docs or direct memories, not Brainfile-only records.
- `created_at_order`: larger numbers are newer for wake/list ordering tests.
- `metadata.validity`: planned freshness state: `active`, `historical`,
  `rejected`, `superseded`, `stale`, or `unknown`.
- `metadata.claim_key`: stable claim family used by stale/supersession tests.
- `metadata.superseded_by`: fixture ID that replaces this memory.

The eval seeder writes these lifecycle values into Recoil's structured store
columns as well as retaining the original metadata JSON.

## Case Lines

Case records have `type: "case"`:

```json
{"type":"case","id":"sqlite-driver-current","mode":"search","query":"what sqlite library do we use?","scope":{"kind":"project","id":"project-recoil"},"limit":5,"expected_current_ids":["mem_sqlite_mattn"],"expected_historical_ids":["mem_sqlite_swlote"],"forbidden_current_ids":["mem_sqlite_swlote"]}
```

Case fields:

- `category`: one of `current_decision`, `stale_history`, `absent_fact`,
  `scope_isolation`, or `wake_safety`.
- `mode`: `search` or `wake`.
- `query`: query string. Wake can use an empty query.
- `scope`: the scope passed to the command.
- `limit`: result limit.
- `expected_current_ids`: memories that must appear as current/primary evidence.
- `expected_historical_ids`: memories that may appear only as labeled history.
- `forbidden_current_ids`: memories that must not be presented as current truth.
- `forbidden_ids`: memories that must not appear anywhere in the result.
- `expected_empty`: true when the right behavior is no answer.

The harness treats current/primary search results as the scoring surface.
Historical search matches can still be shown separately by the CLI without
counting as current guidance.

## Initial Metrics

The first harness should report:

- recall@k for `expected_current_ids`.
- MRR for the first expected current result.
- empty-result accuracy for `expected_empty`.
- scope isolation failures when `forbidden_ids` leak across scopes.
- stale demotion when `forbidden_current_ids` rank as current evidence.
- wake safety when `wake` includes superseded or rejected memories.

Sanity-check the fixture syntax with:

```sh
jq -c . eval/fixtures.jsonl >/dev/null
```
