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

### Reproducing

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

Outputs land in `bench/results/longmemeval_<timestamp>.{jsonl,md}` and
`longmemeval_<timestamp>_summary.json`.
