# recoil bench results

## LongMemEval_S — baseline

**recoil at v0, no embeddings, no LLM, no reranking — plain SQLite FTS5 with the
default ranking formula in `internal/store/store.go:340-355`.**

- Dataset: `longmemeval_s_cleaned.json` (500 questions, ~48 sessions per question,
  ~115K tokens of haystack each, 30 abstention)
- Run: 2026-05-12
- Top-K: 10
- Granularity: session (one memory per haystack session)
- Hardware: 24-core Linux, AMD64
- Total runtime: 1m 49s for 500 questions

### Headline

| Metric | Value |
|---|---:|
| **Recall@5** | **0.9723** |
| Recall@10 | 0.9894 |
| MRR | 0.9299 |
| Latency p50 / question | 188 ms |
| Latency p95 / question | 228 ms |

This is *retrieval recall* — does the ground-truth `answer_session_ids` appear in
the top-K returned. Not QA accuracy. Comparable to MemPal's R@5; not comparable
to systems reporting end-to-end QA (Mastra, Supermemory, Mem0).

### Head-to-head (R@5 retrieval recall)

| System | R@5 | LLM | Embeddings |
|---|---:|---|---|
| MemPal hybrid v4 + Haiku rerank | 1.000* | Haiku | all-MiniLM-L6-v2 |
| MemPal hybrid v3 + Haiku rerank | 0.994 | Haiku | all-MiniLM-L6-v2 |
| MemPal hybrid v2 (no LLM) | 0.984 | — | all-MiniLM-L6-v2 |
| MemPal hybrid v1 (no LLM) | 0.978 | — | all-MiniLM-L6-v2 |
| **recoil baseline (FTS5 only)** | **0.972** | **—** | **—** |
| MemPal raw ChromaDB | 0.966 | — | all-MiniLM-L6-v2 |
| Stella V5 1.5B (dense retriever) | ~0.85 | — | yes |
| Contriever | ~0.78 | — | yes |
| BM25 (sparse keyword) | ~0.70 | — | — |

*MemPal's 1.000 figure was tuned on 3 specific failure cases per their own
benchmark-integrity caveat; their honest held-out generalisable number with an
LLM in the loop is 0.984 on 450 unseen questions.

### Per question type

| Question type | Count | R@5 | R@10 | MemPal raw R@5 | Δ |
|---|---:|---:|---:|---:|---:|
| knowledge-update | 72 | **1.0000** | 1.0000 | 0.990 | +0.010 |
| single-session-user | 64 | **1.0000** | 1.0000 | 0.957 | +0.043 |
| single-session-assistant | 56 | **1.0000** | 1.0000 | 0.929 | +0.071 |
| temporal-reasoning | 127 | 0.9685 | 0.9921 | 0.962 | +0.007 |
| multi-session | 121 | 0.9587 | 0.9752 | 0.985 | -0.026 |
| single-session-preference | 30 | 0.8667 | 0.9667 | 0.933 | -0.066 |

recoil beats MemPal raw on three categories, ties on temporal-reasoning, loses on
multi-session and preference. The two losses match exactly the two categories
MemPal explicitly called out as needing extra work — they got there with
synthetic preference-extraction documents and quoted-phrase boosting (hybrid
v3/v4 layers).

### Rank distribution (470 scored questions)

| Hit rank | Count | Cumulative |
|---|---:|---:|
| 1 | 414 | 88.1% |
| 2 | 25 | 93.4% |
| 3 | 8 | 95.1% |
| 4 | 5 | 96.2% |
| 5 | 5 | 97.2% |
| 6 | 2 | 97.7% |
| 8 | 2 | 98.1% |
| 9 | 1 | 98.3% |
| 10 | 3 | 98.9% |
| miss (>10) | 5 | 100.0% |

Hits are extremely concentrated at rank 1 — 88% of all scored questions find the
correct session as the top result. The shape favours wake-style boot views: the
first item is almost always the right one.

### The 5 absolute misses

- 3 × `multi-session` — evidence split across sessions where none lexically
  matches the question strongly enough.
- 1 × `single-session-preference` — preference stated indirectly.
- 1 × `temporal-reasoning` — date inference required, FTS5 has no date awareness.

All five fail predictably; none indicate a methodology bug.

### What this measures, and doesn't

- **Measures:** can recoil's FTS5 ranking surface the ground-truth evidence
  session in the top K, when given the literal question as a search query.
- **Doesn't measure:** QA accuracy. Lifecycle / supersession behaviour
  (every memory here is fresh, `validity: unknown`, no claim_key). Cross-scope
  retrieval. Hooks / session-evidence selectors (the harness ingests sessions
  raw, bypassing the coding-tuned selectors).
- **Confound:** results are sensitive to how sessions are formatted at ingest.
  This harness uses plain `"role: content"` lines joined by newlines.
  Alternative formats (with date headers, with turn timestamps) are likely to
  shift the temporal-reasoning number.

## Hybrid retrieval — FTS5 + embeddings

The same harness with `--hybrid-embedding`, fusing FTS5 ranks with cosine
similarity over an embedding model via Reciprocal Rank Fusion (RRF k=60).
All retrieval-only — no LLM in either the write or read path.

### Three providers compared

| Provider / model | R@5 | R@10 | MRR | Cost / 500q | Local? |
|---|---:|---:|---:|---:|---|
| FTS5 only (baseline) | 0.9723 | 0.9894 | 0.9299 | $0 | yes |
| OpenRouter `openai/text-embedding-3-small` | 0.9809 | **0.9936** | 0.9410 | $0.20 | no |
| **Ollama `bge-m3` (local, GPU)** | **0.9830** | 0.9915 | **0.9564** | **$0** | **yes** |

**The local bge-m3 result beats the hosted OpenAI embedding on the headline
R@5 number.** OpenRouter still edges R@10 by 0.0021 (essentially noise — 1
question out of 470). MRR favors bge-m3 by a clear margin (0.9564 vs 0.9410).
Both lift retrieval meaningfully over FTS5 alone.

### Per-category, three-way

| Category | FTS only | OpenRouter | bge-m3 (Ollama) |
|---|---:|---:|---:|
| knowledge-update | 1.0000 | 1.0000 | 1.0000 |
| single-session-assistant | 1.0000 | 1.0000 | 1.0000 |
| single-session-user | 1.0000 | 1.0000 | 0.9844 |
| **single-session-preference** | 0.8667 | 0.8667 | **0.9333** |
| temporal-reasoning | 0.9685 | 0.9764 | 0.9764 |
| multi-session | 0.9587 | 0.9835 | 0.9835 |

The category where bge-m3 wins decisively is **single-session-preference
(+6.6pp over both FTS-only and OpenRouter)**. Preference questions are
stated indirectly ("I find Postgres more reliable in my experience" → "what
does the user prefer for databases?") and require semantic inference that
keyword overlap can't capture. bge-m3 evidently captures this kind of
soft-paraphrase relationship better than text-embedding-3-small does on
this benchmark.

Multi-session and temporal-reasoning gain identically with either embedding
model — they're driven by retrieval-recall on multi-evidence questions,
which both models solve.

### Why local bge-m3 wins on this benchmark

Two plausible factors, in order of likelihood:

1. **Dimension and architecture.** bge-m3 is a 568M-param multi-vector
   model with 1024-d embeddings trained for retrieval; `text-embedding-3-small`
   is a smaller 1536-d model trained for general semantic similarity. bge-m3
   was tuned harder for the BEIR/MTEB retrieval suite.
2. **Long context.** bge-m3's 8K context handles entire sessions natively;
   text-embedding-3-small's 8191-token limit also fits, so this probably
   doesn't drive the gap on this dataset — but it would matter for longer
   memories.

The headline takeaway: **a free, locally-served embedding model beats a paid
hosted one on the canonical academic memory benchmark.** That is not a
universal claim — different datasets, different distributions, different
ranking. On LongMemEval_S, with recoil's hybrid retrieval, bge-m3 is the
right default for users who can run a 1.2 GB model locally.

### Bench wall-clock by provider

| Provider | 500-question run time |
|---|---:|
| OpenRouter (batched API) | ~10 min |
| Ollama bge-m3 (local, 3090) | ~37 min |
| FTS-only (no embedding step) | ~2 min |

OpenRouter is faster end-to-end because the API parallelizes batches across
their infrastructure. The local bge-m3 throughput on a single 3090 caps at
~25K tokens/sec, which sets the floor. **For real production query latency
this difference disappears** (one query embedding instead of 500 × 50
fresh batches); see the production-latency section below.

## Production query latency (what users actually feel)

The bench numbers above are stress tests — every question re-embeds 50
sessions × ~10K chars from scratch. **That is not what recoil does in
practice.** Embeddings are computed once at write time and stored in
`memory_embeddings`; at query time recoil embeds only the user's question
(one inference) and does cosine over already-stored vectors.

The realistic operating point, measured locally on a 3090 with Ollama
nomic-embed-text against a 1000-memory project:

| Operation | Latency (median, 5 runs) |
|---|---:|
| One-time index of 1000 memories | 21.25 s (~21 ms / memory) |
| FTS-only search | **28 ms** (range 26–35) |
| Hybrid search (FTS + local embedding) | **263 ms** (range 255–273) |

Hybrid overhead is almost entirely one local Ollama call to embed the
query. Cosine similarity over 1000 stored vectors is microseconds — not
even visible in the latency.

### Bench-vs-production breakdown

| | LongMemEval bench | Real production |
|---|---|---|
| Embeddings per query | 50 sessions × ~10K chars (~125K tokens) | 1 query × ~50 chars (~12 tokens) |
| Are embeddings cached? | No — re-embedded every question | Yes — stored at write time |
| Per-query Ollama calls | ~50 inputs in a batch | 1 input |
| Wall time per query | ~5 s (bge-m3) | **263 ms** (nomic) |

The 100–1000× gap between bench wall time and production query latency is
not a contradiction — it's the difference between "re-index everything
every search" (worst case) and "amortized index, cheap query" (real case).

### Production Ollama setup notes

Three gotchas worth pinning in the README when documenting the local path:

1. **First query after idle is slow.** Ollama unloads models after ~5 min
   by default; the cold-load can take 15–30 s. For agent workflows that
   expect sub-second response, set `OLLAMA_KEEP_ALIVE=24h` or send a
   warmup ping at agent startup.
2. **Write throughput, not query throughput, is the bottleneck.** A 3090
   sustains ~5–10 docs/sec on bge-m3 (long sessions) or ~50 docs/sec on
   nomic-embed-text (short memories). Bulk re-indexing 10K memories takes
   a few minutes. Interactive queries are sub-second forever after.
3. **CPU-only environments work but the budget is tighter.** nomic on
   CPU is ~200 ms per embed; hybrid query latency lands around 500–800 ms.
   Still acceptable for agent search, marginal for tight tool loops.

### Reproducing retrieval

```sh
# 1. Get the dataset (~277 MB)
mkdir -p bench/.corpus
curl -fSL -o bench/.corpus/longmemeval_s_cleaned.json \
  https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json

# 2. Build the harness
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o /tmp/recoil-bench ./bench/

# 3. Run
/tmp/recoil-bench longmemeval
```

## LongMemEval_S — end-to-end QA

The retrieval number above answers "did recoil surface the right session in
top-K." The QA number answers "given recoil's retrieval, did the system
produce a correct final answer." QA is the user-visible outcome; R@K is the
diagnostic that decomposes it.

- Dataset: same `longmemeval_s_cleaned.json` (500 questions, 30 abstention)
- Retrieval: recoil FTS5, top-5 (matches headline R@5)
- Prompt: paper-default `history_format=json`, `useronly=false`, Chain-of-Note
  ("Answer step by step: first extract all the relevant information, and then
  reason over the information to get the answer.")
- Grader: `openai/gpt-4o-mini-2024-07-18`, paper-exact prompts copied verbatim
  from `evaluate_qa.py` (5 per-question-type templates + abstention template)
- Transport: OpenRouter, single `OPENROUTER_API_KEY`

### Headline QA — three answerers, identical recoil retrieval (K=5)

| Answerer | Overall acc. | Incl. abstention | Cost (500q) | Reasoning |
|---|---:|---:|---:|---|
| `openai/gpt-4o-mini-2024-07-18` | 0.6979 | 0.6960 | $1.15 | none |
| `anthropic/claude-haiku-4.5` | 0.7872 | 0.7940 | $8.86 | thinking off |
| **`openai/gpt-5-mini`** | **0.8170** | **0.8160** | **$2.06** | minimal |

gpt-5-mini is the cost/quality frontier on this benchmark: +3 points over
Haiku 4.5 at 4.3× lower cost, +12 points over gpt-4o-mini at ~1.8× the cost.
The 2026 reasoning-mini tier dominates the 2024 chat-mini tier.

### Retrieval ablation — gpt-5-mini answerer, five context modes

The single most important QA experiment. Same answerer, same grader, same
prompt format; only the *what gets passed to the LLM* changes. This isolates
recoil's retrieval as the variable.

| Mode | Overall acc. | Cost | What it tells us |
|---|---:|---:|---|
| no-retrieval (cold) | 0.0468 | $0.25 | floor — gpt-5-mini knows nothing about these users from training |
| full-context (all ~48 sessions) | 0.7809 | $14.12 | "throw everything at the LLM" baseline |
| recoil retrieval, K=5 | 0.8170 | $2.06 | top-5 retrieval headline |
| **recoil retrieval, K=10** | **0.8489** | **$3.55** | **best non-oracle setting** |
| oracle (labelled sessions only) | 0.9191 | $0.90 | ceiling for perfect retrieval |

**The load-bearing finding: recoil-K=10 beats full-context by 6.8 points at
4× lower cost.** Passing fewer but better-targeted chunks to gpt-5-mini
produces *more accurate* answers than passing every session. Retrieval is not
a complexity-vs-cost tradeoff here; it is a quality multiplier. The simplest
possible interpretation: noise hurts gpt-5-mini's reasoning more than missing
context does, at this haystack size. recoil's job is to lower the noise floor.

The recoil-K=10 score is within 7 points of perfect retrieval (oracle 0.919)
and within 10 points of Mastra's published 0.9487 — and we matched their
answerer, so that gap is entirely architecture above the retrieval primitive.

### Per-category, gpt-5-mini, all five modes

| Category | floor | K=5 | K=10 | full | oracle |
|---|---:|---:|---:|---:|---:|
| single-session-user | 0.016 | 1.000 | 1.000 | 1.000 | 0.984 |
| single-session-assistant | 0.286 | 0.964 | 0.982 | 0.982 | 1.000 |
| single-session-preference | 0.033 | 0.900 | 0.967 | 0.767 | 0.967 |
| knowledge-update | 0.028 | 0.861 | 0.903 | 0.792 | 0.903 |
| temporal-reasoning | 0.008 | 0.795 | 0.803 | 0.685 | 0.866 |
| multi-session | 0.008 | 0.628 | 0.694 | 0.669 | 0.901 |
| abstention | 1.000 | 0.800 | 0.833 | 0.833 | 0.800 |

Three notable category effects:

1. **Preference loses 20pp on full-context vs K=10** (0.767 vs 0.967). Indirect
   preferences get washed out when the LLM has too many irrelevant sessions
   to weigh.
2. **Temporal-reasoning loses 12pp on full-context vs K=10** (0.685 vs 0.803).
   Date disambiguation is harder when many sessions compete for the answer.
3. **Multi-session is the residual hard problem**: K=10 hits 0.694, oracle
   hits 0.901. The 21pp gap is *retrieval recall* on multi-evidence questions,
   not LLM reasoning — this is where future retrieval improvements have the
   highest leverage.

### Cost-accuracy curve

```
acc
0.95 │                                          ● oracle ($0.90)
0.90 │
0.85 │                               ● K=10 ($3.55)
0.80 │                        ● K=5  ● full-context ($14.12)
0.75 │
0.70 │
...
0.05 │● no-retrieval ($0.25)
     └────┬────┬────┬────┬────┬────────────┬────
       $0   $2   $4   $6   $8           $14   cost
```

The cost-accuracy frontier is **K=10**, not full-context. The full-context
point is dominated — both more expensive and less accurate. The oracle point
is purely informational (you don't have ground-truth session IDs at query
time in production); the only realistic operating points are along the
no-retrieval / K=5 / K=10 line.

### Retrieval-to-answer decomposition — recoil-K=5, three answerers

| Category | recoil R@5 | gpt-4o-mini | Haiku 4.5 | **gpt-5-mini** |
|---|---:|---:|---:|---:|
| single-session-assistant | 1.0000 | 1.0000 | 1.0000 | 0.9643 |
| single-session-user | 1.0000 | 0.9688 | 0.9531 | **1.0000** |
| knowledge-update | 1.0000 | 0.8056 | 0.8472 | **0.8611** |
| temporal-reasoning | 0.9685 | 0.6378 | 0.7717 | **0.7953** |
| multi-session | 0.9587 | 0.4876 | 0.5950 | **0.6281** |
| single-session-preference | 0.8667 | 0.4000 | 0.7333 | **0.9000** |
| abstention | n/a | 0.6667 | 0.9000 | 0.8000 |

The single-session-preference jump (0.40 → 0.73 → 0.90) is the clearest signal
of reasoning-model uplift: preference statements are usually indirect ("I find
Postgres more reliable in my experience") and require inference rather than
extraction. gpt-5-mini's reasoning step closes that gap almost entirely.

### What the gap means

- **Retrieval is a quality multiplier, not just a cost lever.** The clearest
  evidence is the ablation table: K=10 recoil retrieval beats full-context by
  6.8 points at 4× lower cost. Adding more sessions past the relevant ones
  *degrades* gpt-5-mini's reasoning. recoil's job is to lower the noise floor;
  the LLM does the synthesis.
- **The answerer dominates the QA score at fixed retrieval.** Same K=5 chunks
  fed to gpt-4o-mini vs gpt-5-mini = 12-point spread (0.698 vs 0.817). Biggest
  gains: preference (+50pp), temporal (+16pp), multi-session (+14pp) — all
  categories that require inference, not just extraction.
- **The architecture-vs-Mastra gap is now bounded.** With the same answerer
  (gpt-5-mini) Mastra reports 0.9487, we get 0.8489 at K=10. That ~10pp gap
  is attributable to their write-time LLM observer + likely larger context
  window, not the model or the retrieval primitive. Whether to close that
  gap is a v1 architecture question, not a retrieval-quality question.
- **Per-question latency.** gpt-5-mini at reasoning=minimal hits p50 = 3.9s
  for K=5/K=10 and p50 = 6.5s for full-context (more context = slower).
  Cheaper per question than Haiku 4.5 (p50 = 4.0s) at higher accuracy.

### Honest caveats vs published numbers

- Mastra reports **94.87%** QA accuracy on LongMemEval with GPT-5-mini.
  Supermemory reports ~99% with an 8/12-agent ensemble. With the **same
  answerer** (`openai/gpt-5-mini`) our system scores 0.817 — a 13-point gap
  to Mastra. The gap is *not* answerer choice (we matched them); it is
  architecture above the retrieval primitive. Likely contributors: their
  LLM-observer pre-extracts facts at write time (we ingest verbatim), they
  likely pass more context than top-5 (we use K=5 to match our R@5 number),
  and their prompt scaffolding may be more sophisticated. Closing the gap
  is an *engineering* problem, not a model problem.
- **The QA score is not directly attributable to recoil.** It is the
  composition of (recoil retrieval) × (answerer LLM) × (Chain-of-Note prompt)
  × (gpt-4o-mini grader). The fair attribution is the decomposition table
  above: recoil's contribution is the R@5 column, which is 0.972.
- This run did not test recoil's lifecycle features. Every memory ingested was
  fresh, `validity: unknown`, no claim_key, no supersession. The lifecycle
  hypothesis (does supersession improve grounding?) remains untested.

### Reproducing QA

```sh
export OPENROUTER_API_KEY=sk-or-v1-...

# Answerer pass (recoil retrieval + LLM)
/tmp/recoil-bench longmemeval-qa \
  --mode recoil-k5 \
  --answerer anthropic/claude-haiku-4.5 \
  --concurrency 10

# Grade the hypotheses (LLM-as-judge, paper-exact prompts)
/tmp/recoil-bench longmemeval-grade \
  --hyp bench/results/qa_recoil-k5_anthropic_claude-haiku-45_*.jsonl \
  --grader openai/gpt-4o-mini-2024-07-18 \
  --concurrency 10
```

Per-run artifacts land in `bench/results/`. The graded JSONL matches
LongMemEval's `eval_results` schema, so the official Python `evaluate_qa.py`
can also consume our hypothesis JSONL directly if exact paper-grading
parity is needed.

---

## LoCoMo (snap-research, ACL 2024)

LoCoMo (`locomo10.json`) is the long-term conversational memory benchmark
from Maharana et al, ACL 2024 ([arXiv:2402.17753](https://arxiv.org/abs/2402.17753)).
10 records, each a 19–35 session conversation between two named speakers
with ~420 turns and ~9k tokens, and ~199 QA pairs. Evidence is at the
turn level via `dia_id` markers like `D1:3` (session 1, turn 3).

Run with:

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench locomo
```

Dataset: `bench/.corpus/locomo10.json` (2.8 MB; download once from
`https://github.com/snap-research/locomo/raw/main/data/locomo10.json`).

### Score (recoil FTS5 + signal rerank, no LLM, no embeddings)

| grain | R@5 | R@10 | MRR |
|---|---|---|---|
| **turn-grain** (top-K contains exact evidence dia_id) | **0.4942** | 0.5857 | 0.4799 |
| **session-grain** (top-K contains correct session) | **0.8026** | 0.8896 | — |

Scored on 1540 non-adversarial questions across all 10 records;
446 category-5 abstention questions are reported separately.

### Per category

| category | n | turn R@5 | turn R@10 | session R@5 | session R@10 |
|---|---|---|---|---|---|
| open-domain | 841 | 0.5493 | 0.6314 | **0.8763** | 0.9358 |
| temporal-reasoning | 321 | 0.5701 | 0.6760 | 0.7757 | 0.8754 |
| single-hop | 282 | 0.3191 | 0.4255 | 0.7057 | 0.8369 |
| multi-hop | 96 | 0.2708 | 0.3542 | 0.5312 | 0.6875 |

### Latency

- Search p50: 7 ms · p95: 11 ms (search-only, ingest excluded)
- Ingest: per-record fresh SQLite store, ~420 turns each → all 10 records
  in well under a minute on a 3090

### What the numbers mean

The turn-grain score is the strict apples-to-apples retrieval metric: a hit
counts only if the precise dia_id cited by the LoCoMo evidence appears in
recoil's top-K. The session-grain score is the operational metric — for a
real memory system handing context to an LLM, surfacing the correct session
(any turn in it) is what matters in practice, because the LLM then reads the
session and finds the specific fact.

**Direct comparison vs the literature is non-trivial.** Mem0's published
LoCoMo number (91.6) is an end-to-end LLM-graded answer score, not retrieval
recall — they retrieve, generate an answer, and have an LLM judge whether
the answer is right. Our 0.8026 session-grain R@5 is the *retrieval* leg
only. The right comparison would be:

  - Mem0 retrieval R@5 (not published as a standalone number)
  - vs recoil retrieval R@5 (this table)

For an end-to-end answer comparison, recoil would need the same answerer +
grader pipeline that the LongMemEval-QA harness already implements; that is
a follow-up.

### Observations

- **Multi-hop is the weakest category** (0.53 session R@5). Expected — multi-
  hop requires combining facts from multiple turns, but our retrieval
  surfaces sessions independently. A session-fusion step or a per-question
  multi-leg query would be the natural fix.
- **Single-hop is also weaker than open-domain** at session-grain (0.71 vs
  0.88). Single-hop questions are often phrased with very specific language
  that doesn't share vocabulary with the source turn (e.g. "What did
  Caroline research?" → the source turn says "I'm looking into adoption
  agencies"). Embedding-based retrieval (which we have via `--hybrid`)
  would close this gap.
- **Temporal reasoning is reasonable** (0.78 session R@5) — `retrieval.
  TemporalScore` plus the lookback-window / target-date heuristics in
  `internal/retrieval/signals.go` are doing real work here.
- **Open-domain dominates** because these questions tend to share vocabulary
  with the source turn — exactly what FTS5+BM25 is built for.

This is the FTS-only baseline. A hybrid run (`--hybrid` equivalent for
LoCoMo) is a natural next experiment, expected to close the single-hop and
multi-hop gaps the same way it did on LongMemEval.

---

## BEAM (Tavakoli et al, ICLR 2026)

BEAM (Beyond a Million Tokens, [arXiv:2510.27246](https://arxiv.org/pdf/2510.27246))
is the memory benchmark designed for production-scale agent contexts.
Conversations exist at four token scales (100K, 500K, 1M, 10M) and span 10
probing-question categories: abstention, contradiction_resolution,
event_ordering, information_extraction, instruction_following,
knowledge_update, multi_session_reasoning, preference_following,
summarization, temporal_reasoning. Each non-abstention question has
`source_chat_ids` — the integer turn IDs that justify the answer.

Recoil's BEAM retrieval score: did top-K include at least one source turn ID?

Run with:

```sh
bash bench/fetch_beam.sh 100K
bash bench/fetch_beam.sh 1M       # ~168 MB
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench beam --scale 100K
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench beam --scale 1M
```

### 100K scale (35 conversations, 5732 turns)

| metric | value |
|---|---|
| R@5 | **0.5042** |
| R@10 | 0.5944 |
| MRR | 0.4434 |
| scored questions | 355 (40 abstention) |
| ingest total | 6.16 s |
| search p50 / p95 | 22 ms / 49 ms |

Per category:

| category | n | R@5 | R@10 |
|---|---|---|---|
| contradiction_resolution | 40 | **0.950** | 1.000 |
| temporal_reasoning | 40 | 0.850 | 0.975 |
| knowledge_update | 40 | 0.825 | 0.925 |
| information_extraction | 40 | 0.675 | 0.700 |
| multi_session_reasoning | 40 | 0.575 | 0.725 |
| summarization | 36 | 0.306 | 0.444 |
| preference_following | 39 | 0.180 | 0.308 |
| event_ordering | 40 | 0.075 | 0.150 |
| instruction_following | 40 | 0.075 | 0.100 |

### 1M scale (35 conversations, 74630 turns)

| metric | value |
|---|---|
| R@5 | **0.3702** |
| R@10 | 0.4647 |
| MRR | 0.3988 |
| scored questions | 624 (70 abstention) |
| ingest total | 85.37 s |
| search p50 / p95 | 62 ms / 120 ms |

Per category:

| category | n | R@5 | R@10 |
|---|---|---|---|
| contradiction_resolution | 70 | **0.843** | 0.900 |
| knowledge_update | 70 | 0.743 | 0.814 |
| temporal_reasoning | 70 | 0.543 | 0.743 |
| information_extraction | 70 | 0.343 | 0.400 |
| multi_session_reasoning | 70 | 0.343 | 0.529 |
| summarization | 66 | 0.197 | 0.394 |
| preference_following | 69 | 0.130 | 0.174 |
| event_ordering | 69 | 0.087 | 0.101 |
| instruction_following | 70 | 0.086 | 0.114 |

### Observations

The shape of the per-category numbers matches what LoCoMo and LongMemEval
have already shown: BM25-style retrieval handles factual recall well and
struggles with reasoning. Specifically:

- **High-performing** (R@5 > 0.5 at 1M): contradiction_resolution,
  knowledge_update, temporal_reasoning. These have specific entities and
  date language that recoil's existing scoring picks up directly.
- **Low-performing** (R@5 < 0.2 at 1M): event_ordering,
  instruction_following, preference_following. These don't have a single
  evidence turn — they require state tracking across many turns or
  reasoning about user preferences/instructions in aggregate. Pure
  retrieval can't surface the right answer when there isn't one specific
  passage to find.
- **Scale degradation**: R@5 drops from 0.50 (100K) → 0.37 (1M). The
  biggest single-category drops are information_extraction (-33pp) and
  temporal_reasoning (-31pp). At 10× more turns, noise increases and FTS
  alone loses signal; this is where the existing `--hybrid` embedding
  path would close the gap.
- **Latency stays sane**: search p95 went 49 ms → 120 ms going from
  ~5,700 → ~75,000 ingested turns. Linear-ish scaling, well within
  interactive bounds. Ingest at 1M scale = ~870 turns/sec via direct
  store.AddMemory (no CLI overhead).

### What the numbers do not say

Mem0's published BEAM numbers (64.1 at 1M, 48.6 at 10M) are end-to-end
LLM-graded answer scores. They retrieve, generate an answer, and judge it
with an LLM. We're measuring the retrieval leg only — given the right
source turn ID, did recoil surface it in top-K. Mem0's published number
includes the answer-generation step as well as ranking quality beyond
just "did the right turn appear in top-K." The fair direct comparison
would be Mem0's retrieval R@5 (not published as a standalone number).

### 10M scale

Not run in this pass. The 10M dataset is ~1.4 GB and based on the 1M
trend would take ~15 minutes ingest plus longer search per question.
The harness supports it via `--scale 10M` once the data is fetched with
`bash bench/fetch_beam.sh 10M`; documenting here as the obvious next
experiment.

---

## End-to-end QA pipeline (apples-to-apples vs Mem0)

Mem0's published LoCoMo (91.6) and BEAM (64.1 / 48.6) numbers are
**end-to-end LLM-graded answer scores**, not retrieval R@K. To compare
recoil to those numbers fairly we run the same three-step pipeline:

1. **Retrieve** with recoil (FTS5 + signal rerank, or hybrid FTS+embeddings)
2. **Answer** by handing the retrieved context to an LLM
3. **Grade** the answer with an LLM-as-judge using rubric-aware prompts

This is wired into `bench/` for both LoCoMo and BEAM, mirroring the
existing `longmemeval-qa` + `longmemeval-grade` pipeline.

### LoCoMo (commands)

```sh
export OPENROUTER_API_KEY=sk-or-v1-...

# Step 1+2: retrieval -> answerer LLM. Writes hypothesis JSONL.
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench locomo-qa \
  --mode recoil-k10 \
  --answerer openai/gpt-4o-mini-2024-07-18 \
  --concurrency 8

# Optional: hybrid retrieval (FTS5 + embedding cosine, RRF-fused)
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench locomo-qa \
  --mode recoil-k20 \
  --hybrid-embedding \
  --embed-provider ollama \
  --embed-model bge-m3 \
  --answerer openai/gpt-4o-mini-2024-07-18

# Step 3: grade hypotheses
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench locomo-grade \
  --hyp bench/results/locomo_qa_recoil-k10_*.jsonl \
  --grader openai/gpt-4o-mini-2024-07-18
```

### BEAM (commands)

```sh
# Step 1+2 at 100K scale
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench beam-qa \
  --scale 100K \
  --mode recoil-k10 \
  --answerer openai/gpt-4o-mini-2024-07-18 \
  --concurrency 8

# Step 1+2 at 1M scale (longer; ingest alone is ~85s)
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench beam-qa \
  --scale 1M \
  --mode recoil-k20 \
  --hybrid-embedding \
  --embed-provider ollama \
  --embed-model bge-m3

# Step 3: grade
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench beam-grade \
  --hyp bench/results/beam_qa_100k_*.jsonl \
  --grader openai/gpt-4o-mini-2024-07-18
```

### Why this is now apples-to-apples vs Mem0

Both pipelines now share the same three stages: retrieve → generate → judge.
The only difference is **whose retrieval system feeds the LLM**. Mem0 publishes
their pipeline's number; running this harness with the same answerer + grader
gives recoil's pipeline number directly comparable to Mem0's 91.6 / 64.1 / 48.6.

The retrieval-leg numbers we already have (LoCoMo 0.8026 session R@5, BEAM 1M
0.3702 turn R@5) are the lower bound. Adding the LLM stages should:
  - close the gap on reasoning-heavy categories (event_ordering,
    instruction_following) because the LLM can synthesize an answer from
    multiple retrieved turns
  - widen the gap on lookup categories (knowledge_update, contradiction_
    resolution) because the LLM filters the relevant turn out of a pool

### What we publish

Once the QA pipeline runs at full scale, the README/RESULTS will be updated
with two columns per benchmark: "retrieval R@5" and "end-to-end answer
accuracy (LLM-graded, gpt-4o-mini judge)". The latter is the column directly
comparable to Mem0's published numbers. We commit the JSONL hypothesis files
under bench/results/ so the comparison is auditable.

### Hybrid retrieval flag set (LoCoMo + BEAM)

Both `locomo-qa` and `beam-qa` accept the same hybrid flags as
`longmemeval --hybrid-embedding`:

```
--hybrid-embedding           # turn on FTS+embedding RRF fusion
--embed-provider openrouter|ollama
--embed-model <model>        # default text-embedding-3-small or nomic-embed-text
--ollama-host http://...
--max-embed-chars 6000       # cap embedding inputs
--chunk-chars 0              # 0 = truncate, >0 = sliding-window chunk + max-pool cosine
--chunk-overlap 500
--fusion-k 60                # RRF constant
--fts-pool 50                # FTS candidate pool before fusion
--embed-pool 50              # embedding candidate pool
```
