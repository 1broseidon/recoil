# recoil bench

External benchmark harness. Measures recoil against published memory systems
on public datasets. Not part of the CLI surface; runs via `go run ./bench`.

Distinct from `recoil eval`:

- `recoil eval` is the in-CLI coherence gate. Tiny committed fixtures.
  Pass/fail on retrieval properties. Runs in CI.
- `bench/` is the external benchmark. Large downloaded datasets. Comparative
  numbers against published systems. Manual, occasional.

## Layout

```
bench/
├── README.md
├── main.go          subcommand router
├── corpus.go        dataset path resolution
├── longmemeval.go   LongMemEval harness (recall@5/10, per-type breakdown)
├── .corpus/         (gitignored) downloaded datasets
└── results/         (gitignored) per-run JSON + RESULTS.md outputs
```

## Run

```sh
# Download LongMemEval_S (one time, ~277MB)
mkdir -p bench/.corpus
curl -fSL -o bench/.corpus/longmemeval_s_cleaned.json \
  https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json

# Run the benchmark
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench longmemeval

# Override path
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench longmemeval --data /custom/path.json

# Limit to a subset for fast iteration
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go run ./bench longmemeval --limit 25
```

## LongMemEval

500 questions, ~48 sessions per question, ~115K tokens of haystack each.
Each question has labelled `answer_session_ids` — the ground-truth sessions
that contain the evidence.

Metric: **recall@K** — does the labelled session appear in the top K
retrieved? This is *retrieval recall*, not QA accuracy. Comparable to
MemPal's R@5; not directly comparable to systems that report QA accuracy
(Mastra, Supermemory, Mem0).

The 30 abstention questions are excluded from recall scoring (no ground-truth
session).

Reference points:
- MemPal raw ChromaDB: 96.6%
- Stella (dense retriever): ~85%
- Contriever: ~78%
- BM25 (sparse keyword): ~70%
