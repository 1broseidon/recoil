# Stress Eval — Baseline Report

This report is diagnostic, not pass/fail. The point is to surface what breaks
on un-curated, real-shaped data so the next round of work targets measured
defects instead of assumed ones.

Run it with:

```sh
make stress
```

Each corpus under `eval/corpora/<name>/` has a frozen `files/` tree, a
`manifest.yaml`, and a `cases.jsonl` that the existing `recoil eval` harness
seeds and scores. Corpus cases use `match_by: source_path`, so a hit is "any
mined chunk from this file" rather than "this exact fixture ID".

## Baseline metrics

| Corpus | Retrieval | Cases | Pass | Recall@k | MRR | Empty acc | Stale demote fail |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `docs-heavy` | fts | 14 | 10 | 0.9091 | 0.7273 | 0.3333 | 1 |

Default `eval/fixtures.jsonl` FTS baseline remains 14/14 with recall@k 1.0 and
MRR 1.0. The stress run does not regress it.

## docs-heavy

Synthetic Pelican docs (14 markdown files: README, architecture, ADRs,
runbooks, onboarding, FAQ, glossary, roadmap). 14 hand-labelled cases across
five categories. 10 pass, 4 fail.

### Failures

#### `absent-graphql` — **retrieval ceiling**

Query: *"what graphql schema do we expose to clients"*
Expected: empty.
Got: 5 results led by `roadmap.md`.

The corpus never mentions GraphQL. FTS matches on `clients`, `schema`, and
`expose`, none of which are unique enough to suppress. There is no FTS
mechanism to recognize the *primary noun* of the query is absent. This is
exactly the case embeddings or a no-answer threshold are for.

#### `absent-redis` — **retrieval ceiling (sharper version)**

Query: *"what redis configuration do we use"*
Expected: empty.
Got: `architecture/storage.md` first, because storage.md contains the
sentence *"There is no Redis or Memcached in the storage path."*

This is a worse failure than the GraphQL one. The corpus correctly says
"there is no Redis"; FTS surfaces that page as if it *were* the Redis answer.
There is no way for FTS alone to distinguish a presence claim from a
negation. Categorize as retrieval ceiling, but also note the UX implication:
when there is no current claim about X, returning a page that *negates* X is
worse than returning nothing.

#### `contradiction-auth-current` — **foundation behavior (working as
designed, exposing a real limit)**

Query: *"how does the serving layer authenticate clients"*
Expected current: `adr/0004-auth-oidc.md` (the OIDC ADR that supersedes
`0003-auth-jwt.md` in prose).
Got: `adr/0003-auth-jwt.md` first, `0004-auth-oidc.md` second.

Both ADRs say `Status: Accepted`. Neither mined chunk carries `claim_key`,
`supersedes`, or `superseded_by` — the miner does not extract those from
prose, and the supersession story in ADR 0004's "Context" section is plain
English. FTS ranks the JWT ADR slightly higher because the query word
"authenticate" matches its first paragraph more directly.

This is the realistic supersession case: contradictory current docs with no
machine-readable lifecycle. The current store correctly cannot tell them
apart. The fix is *not* to make FTS smarter; it is one of:

1. Teach the miner to extract `claim_key` and supersession hints from ADR
   front-matter or status sections.
2. Add a lightweight authoring convention (`Status: Superseded by ADR 0004`)
   that the miner recognizes.
3. Accept that without authoring discipline, supersession will not work on
   mined corpora — and surface "multiple contradictory current chunks" as a
   first-class retrieval signal in `search`.

This case is the most valuable single finding in the run. It says cleanly:
**lifecycle works when memories are authored with it; mined chunks need a
bridge.**

#### `wake-orientation` — **UX calibration**

Query: empty wake.
Expected: `README.md` somewhere in the eight returned chunks.
Got: 8 chunks, all from non-README files. README's chunk is excluded.

The wake L0/L1/L2 ordering prefers handoffs, decisions, and recency. With a
purely mined corpus and no query, "recency" is the chunk write order, which
puts README at the start, but the wake layering ends up not surfacing it
because the chunk has no role/claim_key to lift it into L1 and the L2 recent
window fills with other files first.

Two reasonable fixes, neither requiring embeddings:

1. Give mined chunks a small recency-independent boost for files that look
   structurally important (root README, `index.md`, `overview.md`).
2. Make unqueried wake's L2 layer round-robin across `source_path` rather
   than take the most-recent-N regardless of provenance — so an 8-slot wake
   on a 14-file corpus surfaces at least one chunk from most files.

The current behavior is not wrong; it just was not designed for the
"unqueried wake against a freshly mined corpus" shape.

### Notable passes

- **`paraphrase-data-import`** (`ingestion` vs `imported`) and
  **`paraphrase-onboarding`** (`new engineer` vs `new hire`) both passed
  with recall 1.0. FTS5 tokenization plus the existing source-path/role
  boosts handle more paraphrase than expected on this corpus. **This is the
  finding that weakens the urgency for embeddings most.** It is also why
  separating embedding fixtures from the FTS baseline matters: the
  paraphrase gap is narrower than the marketing for embeddings suggests.
- **`paraphrase-cache-layer`** passed with MRR 1.0 — FTS surfaced
  `storage.md` for a query about caching even though the doc only mentions
  "key value cache" in a negative context. The role/path boost is doing
  real work here.
- **`contradiction-encryption-at-rest`** passed: the roadmap page ranks
  above storage.md for "encrypted at rest" because storage.md does not
  mention encryption. Useful counter-evidence that contradictions are not
  always FTS failures.
- **`scope-isolation`** passed cleanly. Mining one corpus into one project
  scope and querying from a different scope returned zero results, as
  required.

### Categorized defects

| Defect | Category | Cases | Suggested response |
| --- | --- | --- | --- |
| Absent-fact queries return false positives | retrieval ceiling | `absent-graphql`, `absent-redis` | First real justification for embeddings + a no-answer score threshold. Add to embedding eval set verbatim. |
| Negation handling | retrieval ceiling / UX | `absent-redis` | Out of scope for FTS. Track separately; do not let embedding work hide it. |
| Mined chunks have no `claim_key` / supersession metadata | foundation bug | `contradiction-auth-current` | Extend miner to recognize ADR / status conventions. Keep lifecycle as store property. |
| Unqueried wake misses structurally important files | UX calibration | `wake-orientation` | Either path round-robin in L2 or a small boost for `README.md`/`index.md`/`overview.md`. |

## What this tells us about the embeddings question

Before this run, the embeddings argument was "FTS probably misses
paraphrase." After this run, the embeddings argument is sharper:

- FTS handles vocabulary mismatch on prose better than expected
  (3 of 3 paraphrase cases pass).
- FTS cannot handle absent-fact queries at all (2 of 2 absent cases fail
  for opposite reasons — false positives from generic word overlap, and
  false positives from negation matches).
- FTS cannot tell two contradictory current docs apart without authoring
  metadata. This is not an embeddings problem; embeddings would not help.

So the justified embedding scope, measured: **a no-answer / low-confidence
threshold for absent-fact queries.** Not paraphrase rescue. That is a
narrower, cheaper integration than the maximalist "swap retrieval for
hybrid" framing, and the existing hybrid plumbing is sufficient to test it.

The lifecycle finding is independent and more urgent for the foundation
thesis: until mined chunks can carry `claim_key` and supersession hints,
the stale-aware retrieval guarantee is only true for hand-authored
memories. That is a foundation gap to close before embeddings, not after.

## Next steps (suggested, not done)

1. Add a second corpus shape — a real code repo with sparse docs — to see
   whether the "no-answer threshold" pattern holds when the corpus has
   even less prose density.
2. Add a third corpus — a Zettelkasten-style notes pile — to stress wake
   ordering at higher file counts and overlapping topics.
3. Prototype an ADR-aware miner extension that lifts `Status:` and
   `Supersedes:` into the existing lifecycle fields. Re-run
   `contradiction-auth-current` and confirm it flips to pass without any
   embedding work.
4. Only then evaluate an embedding provider, with `absent-graphql` and
   `absent-redis` as the cases it must improve, and the rest of the
   stress set as the cases it must not regress.
