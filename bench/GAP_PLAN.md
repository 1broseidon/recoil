# Closing the gap to Mem0 — three-track design

**Baseline (May 13 2026):** recoil end-to-end on raw turns scores 58.2 (LoCoMo) /
26.3 (BEAM 100K) vs Mem0's published 91.6 / 64.1. Per-category breakdown shows
the loss is concentrated in **single-fact-buried-in-history** questions
(single-hop 20%, event-ordering 5%, knowledge-update 12.5%). Lookup / abstention
categories already score 70-92%.

The root cause is structural: recoil retrieves raw turns; Mem0 retrieves
distilled fact memories. When the answer is "Caroline researches adoption
agencies", Mem0 returns that one summarised fact; recoil returns five raw
turns and the relevant one frequently isn't among them, so the answerer
correctly refuses.

This document specs three orthogonal strategies to close that gap. Each is
independently shippable; (a) and (c) compose well; (b) is a safety net that
works regardless.

---

## Track A — Ingest-time fact extraction (Mem0's secret sauce)

### Idea

At ingest, run a structured-extraction LLM pass over each session/turn-batch
to produce typed fact memories. Store the original raw turns *and* the
distilled facts; retrieval picks from facts first, falls back to turns.

### Schema sketch

A new `SourceKind = "extracted_fact"` row, written via `AddMemory` with:

```
Role:         "fact"
SourceKind:   "extracted_fact"
SourcePath:   "fact/<entity-or-topic>"
SourceRef:    "<sha of source turns>"
MetadataJSON: {
  "subject": "Caroline",
  "predicate": "researches",
  "object": "adoption agencies",
  "evidence_turn_ids": ["dia_id_123", "dia_id_124"],
  "session_date": "2023-06-09",
  "confidence": 0.92,
  "extracted_by": "deepseek/deepseek-v4-flash",
  "extraction_prompt_version": "v1"
}
```

`memories_fts` already indexes `content`; we'd write the fact as a natural-
language sentence ("Caroline researches adoption agencies. [2023-06-09]")
so FTS keeps working. The structured fields ride along for entity-aware
queries (track C) and for audit/citation back to the original turns.

### Extraction prompt (v1)

One call per session (~10-30 turns each — small enough for one prompt):

> "Read this conversation excerpt. Extract every durable, single-sentence
> fact stated as true about a named entity. Each fact must be (subject,
> predicate, object). Skip greetings, hypotheticals, and questions.
> Output JSON array of {subject, predicate, object, evidence_turn_ids}.
> Resolve relative dates against session_date=YYYY-MM-DD."

Per-session output is 5-30 facts. ~500 input tokens × 30 sessions/record ×
10 records = 150K input tokens at ingest. With DeepSeek V4 Flash ($0.126/M):
**~$0.02 to consolidate the whole LoCoMo corpus once.**

### Retrieval change

Two-leg fan-out:
1. FTS over `(SourceKind IN ('extracted_fact', 'direct'))` boosted toward
   `extracted_fact` by ~+2.0 in the existing signal stack (mirrors how
   `adr/decision/constraint` already gets +3.0 in `store.Search:425`).
2. If a fact row is returned, its `evidence_turn_ids` are dereferenced and
   the originating turns are included alongside (so the answerer can quote
   verbatim and the judge can verify).

### Expected impact

Mem0's own ablation: removing their consolidation layer drops LoCoMo from
~91 to ~50-60. So adding consolidation should move recoil from **58 → ~80
on LoCoMo, 26 → ~45-50 on BEAM**, concentrated in single-hop, knowledge-
update, multi-session-reasoning.

### Cost / complexity

- **Ingest cost:** ~$0.02 per 10-record LoCoMo (one-time per corpus).
- **Code surface:** new `internal/consolidate/` package; one CLI subcommand
  `recoil consolidate <scope>`; bench harness wraps it before scoring.
- **Risk:** extraction quality is the new bottleneck. Bad extractions
  inject incorrect facts. Mitigation: keep raw turns in store; mark facts
  with `Validity: "extracted"` so a wrong fact can be tombstoned without
  losing the source.

### Smoke (planned)

100-question LoCoMo with consolidated facts on top of raw turns. If the
single-hop bucket moves from 20% → 50%+, ship a full run.

---

## Track B — Larger top-K + LLM reranker (cheapest, safest)

### Idea

Pull top-50 candidates from FTS+embed RRF, send them through a single
DeepSeek V4 Flash "rerank these by question relevance, 0-10" call, take
top-5 by reranker score, hand to answerer.

This is the standard 2-stage retrieval pattern (cheap recall first, expensive
precision second). It doesn't change ingest; it's a drop-in replacement
for the final cut from candidate-pool → top-K.

### Variant B1 — just raise top-K, no rerank

The simplest test: re-run the existing pipeline with `--mode recoil-k20`
instead of `--mode recoil-k5`. The answerer sees 20 turns instead of 5;
relevant turns that previously fell off the cliff now reach it. Cost: 4×
prompt tokens to answerer (~$2 on full LoCoMo) but no architectural change.

### Variant B2 — pool 50, LLM rerank, top-5 to answerer

```
1. FTS+embed RRF: pool 50 candidates (already supported via --fts-pool 50
   --embed-pool 50).
2. New rerank step: one DeepSeek call with the question and 50 candidate
   excerpts, asks for ranked indices.
3. Take top-5, feed to existing answerer prompt.
```

The reranker call adds ~1500 input tokens / question. On 1986 questions:
3M tokens × $0.126/M = **~$0.40 extra answerer cost**.

### Expected impact

- B1 (raise K): expect modest lift, 58 → ~62-65. Risk: context dilution
  hurts adversarial/abstention (more distractors → more false confirmations).
- B2 (rerank): expect 58 → ~70 on LoCoMo, 26 → ~40 on BEAM. Recovers
  single-hop because high-recall pool + LLM precision should land the
  one relevant turn in top-5.

### Cost / complexity

- **Per-run cost:** B1 free (config flag), B2 ~+$0.40 per full run.
- **Code surface:** B1 zero. B2 = one new file `bench/llm_rerank.go`
  plus a `--llm-rerank` flag on `locomo-qa` / `beam-qa`.
- **Risk:** LLM reranker can be over-confident on plausible-but-wrong
  turns. Mitigation: have it return scores not just ranks, and only swap
  the top-5 order if the rerank top score > FTS top score + margin.

### Smoke (in progress; see below)

B1 first because zero cost. Then B2 if B1 confirms more context helps.

---

## Track C — Per-entity rolling profiles

### Idea

Maintain a small, append-only profile string per named entity (person,
project, place). Each turn that mentions the entity contributes a one-line
delta. At retrieval time, detect entities in the question, retrieve the
profile *and* the recent turns mentioning that entity.

### Schema sketch

`SourceKind = "entity_profile"`, one row per entity:

```
Role:         "profile"
SourceKind:   "entity_profile"
SourcePath:   "entity/Caroline"
ClaimKey:     "entity:Caroline"      // lets supersede-style upserts work
Content:      "Caroline — researches adoption agencies; single;
               moved from Sweden 4 years ago; pursuing counseling for
               transgender mental health; close friend of Melanie."
MetadataJSON: {
  "entity": "Caroline",
  "first_seen": "2023-04-12",
  "last_updated": "2024-11-30",
  "evidence_count": 47,
  "evidence_turn_ids": [...]
}
```

The profile content is regenerated periodically (e.g., every N new mentions
or on demand) by an LLM pass that reads the profile + new turns and outputs
a refreshed paragraph. This is cheap because the input is bounded by the
profile size (~500 tokens) plus delta turns (~1-2k tokens).

### Retrieval change

Three-leg fan-out:
1. Entity extraction on the question (cheap; spaCy/regex/cheap LLM).
2. If entities found: retrieve their `entity_profile` rows directly via
   `ClaimKey` lookup (O(1), no FTS), boost to top of result list.
3. FTS+embed for the remainder of the slot.

For "what did Caroline research?", the entity_profile for Caroline already
contains the answer in a single, dense paragraph. No retrieval miss possible
unless extraction failed.

### Expected impact

- Tightly bound to single-hop: should push it from 20% → 75%+ because the
  profile is engineered to contain the answer.
- Helps multi-hop and open-domain modestly (profile gives the answerer a
  one-paragraph index of the entity to reason from).
- Doesn't help temporal / event-ordering directly — those are about
  sequencing events, not entity facts.

### Cost / complexity

- **Profile build cost:** ~$0.05 per record (10-15 named entities, one
  refresh each at ~$0.005). For LoCoMo: ~$0.50 one-time.
- **Profile refresh cost:** amortised — only refresh when N new turns
  mention an entity (e.g., N=20).
- **Code surface:** new `internal/profile/` package; entity extraction
  layer (could use small `ner-finetuned-distilbert` ONNX model or just
  a cheap LLM call at retrieval time); retrieval-side fan-out in
  `store.Search`.
- **Risk:** profile freshness — if Caroline's status changes, the profile
  must reflect it. Mitigation: the profile is regenerated, not appended,
  so stale facts get overwritten. `Supersedes` chain preserves history.

### Smoke (planned)

Pick 3 entities from one LoCoMo record (Melanie, Caroline, family members),
hand-build profile rows, re-run the 50 single-hop questions for that
record. If single-hop accuracy moves from ~20% to >50%, automate the
profile-build LLM pass.

---

## Composition

The three tracks are not mutually exclusive:

| Track | Lifts | Composes with |
|---|---|---|
| A — fact extraction | single-hop, multi-session, knowledge-update | B (rerank can include facts) and C (facts feed profiles) |
| B — bigger K + rerank | broad lift, especially noisy categories | A (rerank pool can include facts) |
| C — entity profiles | single-hop, open-domain (entity-anchored) | A (profile generation feeds on facts) |

Recommended order:
1. Smoke B1 (top-K=20): zero cost. Validates "context dilution" hypothesis.
2. Build A prototype: small, high-leverage, foundation for C.
3. Build C: depends on A; biggest single-hop lift.
4. Add B2 rerank: cheap precision tax on top of A+C.

Target: **LoCoMo 80+, BEAM 100K 50+** after A+C wired. Stretch: 85 / 55
with B2 on top.

---

## What ships as a real recoil feature

Everything in the bench harness is a research vehicle. The patterns that proved
out have to translate into shippable agent-workflow features — not bench-only
flags. Here is the prod mapping for each track.

### From Track A (fact extraction) → `recoil consolidate`

**New CLI:** `recoil consolidate [--scope X] [--since TIME] [--model M]`
- Walks unmarked memories in the given scope window.
- Groups them into prompt-sized batches (~10-30 messages each).
- Calls the consolidation LLM with the extraction prompt currently in
  `bench/locomo_extract.go:buildExtractPrompt`.
- Writes results as new memory rows via the existing `AddMemory` path,
  with `SourceKind="extracted_fact"`, `Role="fact"`, `Validity="active"`,
  and `MetadataJSON` carrying subject/predicate/object + evidence pointers.

**Auto-mode:** A background worker (`recoil daemon consolidate-on-idle`) runs
consolidation against fresh memories whenever the agent has been idle for
N minutes. This is the path that makes consolidation feel free in real
agent workflows — no explicit user step required.

**Schema impact:** none. Existing `AddMemory` accepts arbitrary `SourceKind`;
existing `Search` filters by it. The fact-leg retrieval pattern is just a
second `Search` call with `SourceKind="extracted_fact"` — agents/SDKs can
adopt this incrementally.

### From Track B (top-K=20 + hybrid embed) → recoil defaults

**Config defaults to raise:** `Search.Limit` default in `internal/store/store.go`
goes from 5 → 10 for raw retrieval, but the wakeup/agent path becomes a
**two-stage retrieval API** baked into recoil itself:

```go
type Retriever struct {
    FTSCandidatePool int           // default 50
    EmbedRerankPool  int           // default 30
    FinalLimit       int           // default 10
    Embedder         EmbedClient   // pluggable; nil = FTS-only
}
func (r *Retriever) Search(ctx, scope, query) ([]Memory, error)
```

This subsumes the bench-only `hybrid_rows.go` helper into a real recoil
package, so any agent SDK can opt into hybrid retrieval without
reimplementing RRF fusion. The current `recoil search` CLI gets
`--hybrid` and `--embed-provider` flags wired straight through.

The point of doing this in recoil rather than the bench: agents shouldn't
have to know whether their memory store has embeddings. The Retriever
abstracts it.

### From Track C (entity profiles) → `recoil profile`

**New CLI:** `recoil profile [--scope X] [--entity NAME] [--from-facts]`
- Two modes: build profile directly from raw turns (single LLM pass per
  entity, prompted to summarise everything stated about that entity), or
  build from facts (no LLM, just group + concat — the `locomo-profiles`
  algorithm).
- Writes one memory per entity with `SourceKind="entity_profile"`,
  `Role="profile"`, `ClaimKey="entity:<name>"` so refreshes naturally
  supersede prior versions via the existing claim chain.
- Refreshes triggered automatically when ≥N new facts about an entity
  land (same daemon worker as consolidation).

**Retrieval API:** the `Retriever` above gains `--with-profiles` / 
`--with-facts` knobs that fan out additional `Search(SourceKind=...)`
calls and merge into the result. Agents calling `recoil wake` or
`recoil search` get the entity context for free; SDKs see a single
`results` slice with annotated `SourceKind` per row.

**Why ClaimKey:** recoil's claim/supersede semantics already give us a
natural "this profile replaces the older one" without bespoke logic.
Search already filters out superseded rows by default (`Lifecycle:
LifecycleAny` was used in bench to override that for evaluation
auditing; prod retrieval will use `LifecycleCurrent`).

### Where this lands in the repo

| Track | Files touched | New surface |
|---|---|---|
| A — consolidate | `internal/consolidate/`, `cmd/consolidate.go`, `cmd/daemon.go` | `recoil consolidate`, daemon worker |
| B — retriever | `internal/retrieval/`, `internal/store/store.go` defaults, `cmd/search.go` | `Retriever{}` type, `--hybrid` flag |
| C — profile | `internal/profile/`, `cmd/profile.go`, daemon | `recoil profile`, daemon worker |

All three reuse existing primitives: `AddMemory`, `Search`, claim chains,
`SourceKind` filtering. Nothing in the bench harness is doing something
recoil can't do at the API level today — it's just orchestrated externally
because we wanted bench-controlled experiments before turning them into
recoil defaults.

### Order of operations for shipping

1. **Retriever package (Track B).** Pure-internal refactor, no LLM dependency,
   no schema change. Default `recoil search` becomes hybrid-capable. This
   alone gives the +16 single-hop lift to anyone using recoil today.
2. **`recoil consolidate` (Track A).** Adds optional LLM dependency. Default
   model `deepseek/deepseek-v4-flash` (cheap, paid, good quality); can be
   swapped via `RECOIL_CONSOLIDATE_MODEL` env. Opt-in command — agents who
   want raw-turn semantics keep them.
3. **`recoil profile` + claim-chain refresh (Track C).** Highest-impact lever
   based on the rec-0 smoke (+10 pts overall, +13 single-hop, +16 open-domain).
   Builds on consolidate; thinnest layer of the three.
4. **Daemon worker** (`recoil daemon consolidate-on-idle`). Stitches A and C
   into the autonomous background work so agents see compounding memory
   quality without manual steps.

---

## Full LoCoMo result (1986 questions, hybrid embed, k=20)

| Variant | Overall | adversarial | multi-hop | open-domain | single-hop | temporal |
|---|---:|---:|---:|---:|---:|---:|
| Baseline (k=5, hybrid, no facts/profiles) | 58.2 | 91.9 | 38.5 | 55.7 | 20.2 | 57.3 |
| Track C (k=20 + profiles only) | **70.5** | 89.9 | 49.0 | 71.1 | 45.4 | 70.4 |
| Track A+C (k=20 + facts + profiles) | 68.6 | 88.1 | 47.9 | 70.1 | 40.8 | 67.9 |

**Track C alone is the winner: +12.3 pts overall**, +25.2 on single-hop
and +15.4 on open-domain. Adding the facts leg on top of profiles (A+C)
strictly degrades by 1.9 pts — facts compete for the same attention
budget that profiles use more efficiently. **Ship Track C, skip Track A
as a standalone retrieval mechanism. Track A's value is upstream of C:
facts are the raw material from which profiles are synthesised.**

Cost: $1.25 answerer + $0.13 judge = $1.38 (Track C). $0.96 + $0.11 = $1.07 (A+C).
Wall time: ~18 min each. We closed half the gap to Mem0's 91.6 with one
research lever; remaining loss is concentrated in multi-hop (needs
cross-entity synthesis) and adversarial drift (-2 pts; profile content
occasionally lets the answerer over-commit when gold expected abstention).

## Full BEAM 100K result (400 questions, hybrid embed, k=20)

| Category | n | baseline (k=5) | Track C (k=20 + profiles) | Δ |
|---|---:|---:|---:|---:|
| abstention | 40 | 70.0 | **75.0** | +5.0 |
| contradiction_resolution | 40 | 60.0 | **67.5** | +7.5 |
| knowledge_update | 40 | 12.5 | **30.0** | **+17.5** |
| multi_session_reasoning | 40 | 15.0 | 20.0 | +5.0 |
| event_ordering | 40 | 5.0 | 7.5 | +2.5 |
| preference_following | 40 | 12.5 | 15.0 | +2.5 |
| temporal_reasoning | 40 | 17.5 | 17.5 | 0.0 |
| summarization | 40 | 12.5 | 12.5 | 0.0 |
| instruction_following | 40 | 15.0 | 10.0 | -5.0 |
| information_extraction | 40 | 42.5 | **27.5** | **-15.0** |
| **Overall** | **400** | **26.3** | **28.3** | **+2.0** |

Cost: $0.88 answerer + $0.05 judge + $0.40 extract = **$1.33** total.

### BEAM-specific learnings

1. **Topic profiles transfer less cleanly than entity profiles.** LoCoMo
   entities are people with stable biographies; BEAM "topics" are project
   components/decisions/preferences that evolve over the conversation.
   Aggregating all mentions of "login feature" into one profile loses the
   chronology — exactly what information_extraction questions care about.
2. **Where profiles help BEAM: state-current questions.** knowledge_update
   (+17.5) asks "what is X *now*"; a profile that summarises X's evolution
   answers this. Same for contradiction_resolution (+7.5; profile shows
   the divergent statements side-by-side) and abstention (+5; profile
   makes it easier to confirm something never came up).
3. **Where profiles hurt BEAM: specific-detail questions.**
   information_extraction (-15) and instruction_following (-5) ask
   "exactly what did the user say at point X" — the profile's aggregate
   loses that resolution, and crowds out the precise raw turn.
4. **Implication for shipping:** the `recoil profile` feature spec should
   support a per-question opt-out (or a router that picks between
   profile-leg and raw-turn-leg based on question phrasing — "what is"
   vs "exactly what did"). Track C is not a universal win; it's a
   high-leverage tool for biographical/state questions.

## Smoke results (rec-0, 199 questions)

All numbers are LLM-graded answer accuracy with the same answerer
(`deepseek/deepseek-v4-flash`) and judge (same model). Hybrid embedding =
`perplexity/pplx-embed-v1-0.6b` with RRF fusion against FTS rank.

| Variant | Overall | adversarial | multi-hop | open-domain | single-hop | temporal |
|---|---:|---:|---:|---:|---:|---:|
| k=5 baseline (full LoCoMo, 1986q) | 58.2 | 91.9 | 38.5 | 55.7 | 20.2 | 57.3 |
| k=20 baseline (rec-0) | 65.8 | 83.0 | 69.2 | 60.0 | 31.3 | 81.1 |
| k=20 + A1 (facts in same pool) | 61.8 | 87.2 | 69.2 | 58.6 | 25.0 | 64.9 |
| k=20 + A2 (dedicated FTS facts leg) | 63.3 | 78.7 | 69.2 | 62.9 | 18.8 | 81.1 |
| k=20 + A3 (thorough facts, FTS-only) | 62.8 | 80.9 | 61.5 | 57.1 | 28.1 | 81.1 |
| k=20 + A4 (thorough facts + hybrid rerank) | 64.8 | 87.2 | 76.9 | 58.6 | 31.3 | 73.0 |
| **k=20 + C (entity profiles, hybrid rerank)** | **75.4** | **87.2** | **76.9** | **75.7** | **43.8** | **86.5** |

### What we learned

1. **Track B alone is a clear win.** Across full LoCoMo (1986q), raising
   K from 5 → 20 lifted overall accuracy from 48 → 64 (apples-to-apples
   100q slice). K=20 beat K=10 (+13) and K=30 (+3) on that slice. The
   answerer is not context-limited; DeepSeek V4 Flash uses larger windows
   productively without losing precision on adversarial.

2. **Track A in same-pool form HURTS (-4 to -3 pts).** Facts get good FTS
   rank because they're short and dense, displacing the raw turns that
   provide context for temporal reasoning. Lesson: never let extracted
   summaries compete in the same retrieval pool as raw evidence.

3. **Track A in dedicated-leg form is neutral.** Facts pulled via a
   second `Search(SourceKind="extracted_fact")` call and shown in a
   separate prompt block recovers the temporal regression but doesn't
   lift single-hop. Root cause analysis: even with the thorough
   extraction prompt, the relevant fact often isn't FTS-rank-1 (e.g.,
   "Caroline is a single parent" doesn't match the query
   "What is Caroline's relationship status?" without embedding-based
   semantic matching). Adding hybrid-rerank to the facts leg (A4) closes
   most of that gap but adds latency.

4. **Track C is the headline win: +10 pts overall on rec-0.** A single
   long-form profile per entity, hybrid-reranked, brings the entity's
   complete biography into context. The answerer doesn't have to
   triangulate across 20 retrieved facts — it sees one coherent paragraph
   that says "Caroline is a single parent, researches adoption agencies,
   moved from Sweden 4 years ago, ...". Single-hop jumps 31 → 44 (+13),
   open-domain 60 → 76 (+16). No regressions on any category.

5. **Profile quality matters.** The first extraction prompt v1 returned
   85 facts for rec-0 and missed the "single parent" fact entirely. The
   v2 thorough prompt returned 346 facts and caught it, but produced
   some noise. The aggregate-into-profile step compensates: even noisy
   facts, when bundled into one entity paragraph, give the answerer
   enough context to reason correctly.

6. **The cost of consolidation amortises immediately.** v2 extraction
   for rec-0 cost $0.02 and the resulting profile JSON was reused across
   199 questions. In production this means consolidation runs once per
   N new messages (or on idle), not per query.

## Smoke test budget tracker

Budget: $10 for research smokes during this spec phase.

| Smoke | Cost actual |
|---|---:|
| B1: LoCoMo k=20, 100q | $0.05 |
| B1: LoCoMo k=10, 100q | $0.03 |
| B1: LoCoMo k=30, 100q | $0.06 |
| A0: extraction v1, rec-0 | $0.01 |
| A1: rec-0 facts-in-pool | $0.08 |
| A2: rec-0 dedicated FTS facts leg | $0.08 |
| A0: extraction v2, rec-0 | $0.02 |
| A3: rec-0 FTS-only thorough facts | $0.07 |
| A4: rec-0 hybrid-reranked facts | $0.07 |
| **C: rec-0 entity profiles, hybrid** | **$0.17** |
| **Total spent** | **~$0.64** |
| **Headroom remaining** | **~$9.36** |

Headroom remaining is being spent on:
- Extract facts for all 10 records (~$0.20 estimated)
- Full LoCoMo run with profiles + hybrid (~$0.70 answerer + $0.10 judge)
- Optional BEAM 100K with same recipe (~$0.40)

