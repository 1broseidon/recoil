---
id: task-17
title: "FTSQuery: strip stopwords, AND-first with OR fallback; evaluate porter tokenizer"
priority: medium
tags:
  - audit
  - retrieval
  - fts
relatedFiles:
  - internal/store/store.go
  - internal/retrieval/signals.go
parentId: epic-1
createdAt: "2026-06-10T04:40:43.856Z"
completedAt: "2026-06-10T06:05:44.753Z"
updatedAt: "2026-06-10T06:05:44.753Z"
---

## Description
internal/store/store.go FTSQuery (1946-1962) ORs every token including stopwords — 'what is the current auth approach' matches nearly the whole corpus and depends on bm25 + LIMIT 100 to save it; the right chunk can fall outside the limit before any prior runs (already hit on transformers, stress REPORT finding #9). Fix: strip stopwords (stopword() already exists in signals.go); try AND-first leg over significant tokens with OR fallback when AND returns < limit. Separately evaluate FTS5 porter tokenizer (tokenize='porter unicode61') — requires FTS index rebuild migration but structurally eliminates the morphology class (contributor/contributing) currently patched by hand-maintained aliases. Eval-gated.
