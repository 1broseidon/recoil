---
id: task-18
title: Hybrid retrieval auto-on when a fresh local embedding index exists
column: todo
position: 7
priority: medium
tags:
  - audit
  - retrieval
  - embeddings
relatedFiles:
  - cmd/retriever.go
  - cmd/search.go
  - cmd/embed.go
  - internal/embedding/ollama.go
parentId: epic-1
createdAt: "2026-06-10T04:40:44.301Z"
---

## Description
bench/GAP_ANALYSIS.md names hybrid (FTS+embeddings RRF) the smallest-blast-radius experiment toward the 6pp QA gap, yet it ships behind --hybrid with an OpenRouter default needing an API key. A local Ollama provider already exists in internal/embedding/. Fix: auto-enable hybrid when an embedding index exists and is fresh (recoil embed index was run), FTS-only fallback otherwise; config retrieval.mode = auto|fts|hybrid. Agents should not need to know the flag — the binary uses the best index it has.
