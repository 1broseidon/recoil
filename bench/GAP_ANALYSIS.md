# Architecture gap analysis — recoil → Mastra

**Question:** What exactly is needed to close the ~10pp QA gap from recoil
(0.8489 at K=10 minimal) to Mastra's published 0.9487 on LongMemEval_S?

**Honest answer (as of 2026-05-13):** We don't know yet. Of the
production-realistic mechanisms we tested — bigger K, more reasoning,
LLM-rerank — none close the gap. The best single configuration we
measured is **K=10 + reasoning=medium = 0.8894**, still 6 points short.
The simple LLM-rerank we tested actually regressed to 0.8489 because
the reranker over-filtered.

**Budget spent on this analysis:** ~$26 of $25 (slight overrun for the
production rerank validation).

The earlier draft of this document claimed the gap was a "routing problem"
based on a 0.9511 oracle-picker upper bound. **That framing was wrong.**
An oracle picker has access to ground-truth labels; recoil in production
doesn't. When we built and ran an actual production-realistic router
(LLM-rerank with no peeking), the score went *down*, not up. The oracle
ceiling is informative as a theoretical limit but is not directly
achievable by the mechanisms we tried.

---

## 1. Failure decomposition (still valid)

71 non-abstention failures at K=10 minimal break down by mechanism:

| Bucket | Count | What it means |
|---|---:|---|
| Fixed by oracle AND full-context | 19 | retrieval missed; both fixes work |
| Fixed by oracle only | 30 | retrieval missed; full-context can't rescue |
| Fixed by full-context only | 1 | LLM needed more context, not better retrieval |
| Neither fixes it | 21 | reasoning-bounded at minimal effort |

Per-category, multi-session and temporal-reasoning are the leaky buckets.

## 2. What we tried, and what each tells us

| Mechanism | Tested how | Result | What it proved |
|---|---|---:|---|
| Higher K (more candidates) | K=20 minimal vs K=10 minimal | -0.6pp | More context past 10 hurts: noise outweighs signal |
| Higher reasoning effort | K=10 medium vs K=10 minimal | +4.0pp | Internal noise filtering by the LLM works, up to a cap |
| Combine both | K=20 medium vs K=10 medium | -0.4pp | Even with more thinking, K=20 still hurts |
| Cheaper answerer | deepseek-v4-flash K=20 | 0.8723 | Strong cost frontier, ~13× cheaper, still ~6pp short |
| LLM-rerank | K=20 → deepseek-v4-flash → top-5 → gpt-5-mini medium | **-4.0pp from k10med** | **Simple rerank made things worse** |

The rerank attempt is the new data point. The reranker fired on 79% of
questions (the other 20% errored out and fell through to raw K=20). When
it fired, it picked aggressively — 269 of 371 firings kept just 1-2
sessions. For questions that genuinely need 3-5 evidence sessions
(multi-session, temporal-reasoning), this over-filters.

Rerank vs K=10 medium baseline:
- 16 questions rescued (rerank correct, k10med wrong)
- 35 questions lost (rerank wrong, k10med correct)
- Net -19 = -4pp regression

## 3. The production cost ladder, all measured

| Mode | Overall | Cost / 500q | Notes |
|---|---:|---:|---|
| no-retrieval (floor) | 0.0468 | $0.25 | LLM cold |
| gpt-4o-mini K=5 | 0.6979 | $1.15 | 2024 baseline |
| Haiku 4.5 K=5 | 0.7872 | $8.86 | |
| gpt-5-mini full-context | 0.7809 | $14.12 | "throw everything in" hurts |
| gpt-5-mini K=5 minimal | 0.8170 | $2.09 | |
| gpt-5-mini K=20 minimal | 0.8426 | $6.79 | |
| gpt-5-mini K=10 minimal | 0.8489 | $3.55 | |
| **rerank K=20→5** (production) | **0.8489** | $5.40 | **simple rerank doesn't help** |
| deepseek-v4-flash K=10 | 0.8553 | ~$0.30 | |
| deepseek-v4-flash K=10 medium | 0.8660 | ~$0.50 | |
| **deepseek-v4-flash K=20** | **0.8723** | **~$0.50** | cost frontier |
| gpt-5-mini K=20 medium | 0.8851 | $7.44 | |
| **gpt-5-mini K=10 medium** | **0.8894** | **$4.17** | **best production-realistic** |
| oracle gpt-5-mini minimal | 0.9191 | $0.90 | requires labels — not production |
| theoretical 4-config picker (oracle) | 0.9511 | n/a | requires labels — **not achievable** |
| Mastra (published) | 0.9487 | n/a | the target |

## 4. What this means

**recoil's current production frontier is K=10 + reasoning=medium = 0.8894
on gpt-5-mini, or 0.8723 on deepseek-v4-flash K=20 at ~$0.50.**

Neither closes the gap to Mastra. The 6pp residual gap is *real* — it's
not "if only we routed better." It's "we don't currently have a query-time
mechanism that picks the right context per question without peeking."

This forces a re-evaluation of the architectural options. Two
candidate paths remain, both untested by us:

### Path A — write-time fact extraction (Mastra's actual approach)

Have an LLM extract structured facts from each session at write time.
Store facts indexed by their semantic content (claims, dates, entities).
At query time, retrieve relevant facts and expand to full sessions for the
answerer.

This is the architecture I dismissed earlier as "not required." The data
now suggests it may genuinely be the path to 0.95. The dismissal was
based on the oracle picker, which is not achievable.

The cost: an LLM call per session at ingest time. For typical
agent-session loads (a handful of sessions per day) this is cheap — maybe
$0.01 per session with deepseek-v4-flash. The architectural cost is
larger: it adds an LLM dependency to the write path, which recoil
explicitly ruled out as a v0 design principle.

### Path B — smarter query-time architecture (untested ideas)

Several untested mechanisms could plausibly close the gap without a
write-path LLM:

1. **Better rerank prompt**: target a higher minimum picks (e.g. always
   keep 3-5 even if some are uncertain). Our rerank picked 1-2 too
   aggressively on 269/371 firings.
2. **Hybrid retrieval**: FTS5 + embedding similarity, fuse with RRF.
   recoil already has the embeddings sidecar (`internal/embedding/`)
   for this; it's currently optional and unused.
3. **Query expansion**: rewrite the user's question into 2-3 sub-queries
   targeting different aspects, run each, union the results. Particularly
   relevant for multi-session questions.
4. **Iterative retrieval**: retrieve → first-pass answer → use answer to
   formulate refinement query → second retrieve → final answer. Costly
   (2× LLM calls) but production-realistic.

None of these have been tested. Each is a real engineering project.
None are guaranteed to close the gap.

## 5. Honest recommendation

**Stop the gap-closing chase at v0.** The production-realistic frontier
is 0.8894 (K=10 medium), within reach of every Claude/OpenAI-pricing user
for ~$4 per 500 questions. The 6pp gap to Mastra is a v1 architecture
question, not a v0 retrieval-tuning question.

The honest pitch for recoil v0 is:
- 0.972 retrieval R@5 on LongMemEval_S — competitive with the published
  frontier (MemPal raw 0.966), no embeddings, no LLM, no extraction.
- 0.889 end-to-end QA accuracy with gpt-5-mini at reasoning=medium —
  within 6pp of Mastra without a write-time LLM dependency.
- 0.872 with deepseek-v4-flash at ~$0.50 per 500q — 13× cheaper than
  the gpt-5-mini config for ~2pp accuracy cost.

For v1, the data points to two paths to evaluate seriously:
1. **Hybrid retrieval** (FTS5 + embeddings, query-time fusion) — uses
   recoil's existing sidecar, no write-path LLM. This is the
   smallest-blast-radius experiment.
2. **Write-time fact extraction** (Mastra's approach) — adds an LLM to
   the write path, breaks v0 principle, but probably the most direct
   route to ≥0.94.

Run hybrid retrieval first. If it doesn't close the gap, then evaluate
whether Mastra-style fact extraction is worth the architectural cost.

## 6. What to take away from this exercise

The most important meta-finding: **theoretical ceilings calculated from
existing run data (oracle pickers, union-of-correct, etc.) tell you what
*could* be true if you had ground truth — they do not tell you what is
*reachable* in production.** It is easy to fall into the trap of citing a
union ceiling as a "result" when it is actually a label-leaked best-case.

This document originally claimed the gap was a routing problem fixable
by Tier 1 + Tier 2 cheap mechanisms. The production rerank we built
falsified that claim. The honest answer is that we don't have a v0
architecture that reaches Mastra's number, and the path to it is a v1
question that requires deciding whether to break the no-LLM-in-write-path
principle.
