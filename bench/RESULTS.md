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

### Headline QA

| Answerer | Overall acc. | Incl. abstention | Cost (500q) | Reasoning |
|---|---:|---:|---:|---|
| `openai/gpt-4o-mini-2024-07-18` | 0.6979 | 0.6960 | $1.15 | none |
| `anthropic/claude-haiku-4.5` | 0.7872 | 0.7940 | $8.86 | extended thinking off |
| **`openai/gpt-5-mini`** | **0.8170** | **0.8160** | **$2.06** | minimal |

gpt-5-mini is the cost/quality frontier: +3 points over Haiku 4.5 at 4.3× lower
cost, +12 points over gpt-4o-mini at ~1.8× the cost. The 2026 reasoning-mini
tier dominates the 2024 chat-mini tier on this benchmark.

### Retrieval-to-answer decomposition

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
extraction. gpt-5-mini's reasoning step closes that gap almost entirely. The
floor on multi-session (0.628 even with the strongest answerer, against R@5 of
0.959) suggests the limit there is genuine multi-hop synthesis difficulty
rather than the LLM choice — improving it likely needs cross-session
provenance hints in the retrieval output, not a better answerer.

### What the gap means

- **Retrieval is mostly solved.** recoil's R@5 = 0.972 means the right session
  is in top-5 nearly always. The categories where retrieval drops (preference
  0.867, multi-session 0.959) are also the categories where QA drops the
  hardest. Retrieval is the floor; reasoning is the ceiling.
- **The answerer dominates the QA score.** Same recoil chunks fed to two
  different models gave a 9-point spread (0.698 vs 0.787). The biggest gains
  came in the categories that require multi-hop reasoning (multi-session
  +10pp), indirect preference inference (preference +33pp), and abstention
  judgment (+23pp). These are not retrieval problems.
- **Cost vs accuracy trade is steep.** Haiku 4.5 is ~7.7× more expensive than
  gpt-4o-mini and buys +9 accuracy points. That's a real choice, not a
  free lunch.
- **Per-question latency.** Haiku 4.5 was actually *faster* (p50 = 4.0s) than
  gpt-4o-mini (p50 = 6.5s) at this prompt size, despite higher per-token cost.
  The full Haiku run took 4 minutes; gpt-4o-mini took 7 minutes.

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
