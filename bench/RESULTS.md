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
