# Recoil Eval Fixtures

`fixtures.jsonl` defines the first retrieval eval corpus for Recoil. It is
intended for `task-2`, where a harness can seed a temporary database, run
`search`, `list`, and `wake`, and report retrieval quality before ranking or
stale-memory behavior is tuned.

The file is JSON Lines. Each line is a seeded memory, mined corpus, transcript,
or eval case.

Run the harness with:

```sh
recoil eval
recoil eval eval/fixtures.jsonl
recoil eval --suite embeddings --retrieval hybrid
recoil eval --suite workflows
recoil eval --suite session-evidence
recoil eval --suite decisions
recoil eval --suite workflows --out eval/results
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

- `category`: a behavior label such as `current_decision`, `stale_history`,
  `absent_fact`, `scope_isolation`, `wake_safety`, `metadata_retrieval`,
  `faceted_retrieval`, `decision_opposition`, `predicate_coverage`,
  `predicate_composition`, or `cross_domain_matching`.
- `mode`: `search`, `list`, `wake`, or `check`.
- `query`: query string. Wake can use an empty query. Check uses the query to
  find a decision family unless `claim_key` is provided.
- `scope`: the scope passed to the command.
- `role`, `claim_key`, `validity`, `lifecycle`, `source_agent`, and
  `source_path`: optional structured filters.
- `limit`: result limit.
- `include_decisions`: for `wake` cases, include the claim-keyed decision trail
  rendered by `recoil wake --include-decisions`.
- `expected_current_ids`: memories that must appear as current/primary evidence.
- `expected_historical_ids`: memories that may appear only as labeled history.
- `forbidden_current_ids`: memories that must not be presented as current truth.
- `forbidden_ids`: memories that must not appear anywhere in the result.
- `expected_content`: substrings that must appear in the retrieved result text.
  For content-only cases without `expected_current_ids`, all expected substrings
  must appear in one ranked result so the case proves answerable evidence, not
  only scattered corpus coverage.
- `forbidden_content`: substrings that must not appear anywhere in the
  retrieved result text.
- `expected_empty`: true when the right behavior is no answer.

The harness treats current/primary search results as the scoring surface.
Historical search matches can still be shown separately by the CLI without
counting as current guidance.

## Initial Metrics

The first harness should report:

- recall@k for `expected_current_ids`.
- MRR for the first expected current result, or for the first ranked result
  containing all `expected_content` in content-only cases.
- empty-result accuracy for `expected_empty`.
- scope isolation failures when `forbidden_ids` leak across scopes.
- stale demotion when `forbidden_current_ids` rank as current evidence.
- wake safety when `wake` includes superseded or rejected memories.
- wrong-memory failures when forbidden content appears in retrieved text.
- ranked-content failures when content-only expectations are split across
  multiple results instead of appearing in one answerable result.

## Corpus And Transcript Lines

Corpus records mine local files through the same conservative project-file
collector used by `recoil mine`:

```json
{"type":"corpus","id":"docs","path":"corpora/docs-heavy/files","scope":{"kind":"project","id":"project-docs"},"role":"source"}
```

Transcript records run through the session-evidence selector and miner:

```json
{"type":"transcript","id":"session-1","path":"../cli-fixtures/hard/transcripts/agent-memory-01.json","scope":{"kind":"project","id":"workflow-coding"},"source_agent":"codex","session_id":"workflow-agent-memory"}
```

`eval/workflows/` contains the product-facing workflow suite: coding
continuity, non-coding continuity, session-evidence selector behavior,
decision relevance checks, and P3 decision verdict maturity. Use
`recoil eval --suite decisions` for the focused decision suite.
External academic benchmark harnesses stay in `bench/`; large real-repo stress
stays in `stress/`.

`--out` writes normalized JSON and Markdown artifacts. Use a directory to write
the standard file names, or pass a `.json` or `.md` path to choose the stem.
The full product/eval consolidation plan is in `docs/P_SERIES.md`.

Sanity-check the fixture syntax with:

```sh
jq -c . eval/fixtures.jsonl >/dev/null
```
