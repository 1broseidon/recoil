package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

const benchmarkMemoryCount = 10_000

func BenchmarkStoreAddAt10K(b *testing.B) {
	ctx := context.Background()
	st := openBenchmarkStore(b)
	seedBenchmarkMemories(b, ctx, st, benchmarkMemoryCount)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem, duplicate, err := st.AddMemory(ctx, benchmarkMemoryParams(benchmarkMemoryCount+i, "active"))
		if err != nil {
			b.Fatal(err)
		}
		if duplicate {
			b.Fatalf("unexpected duplicate add for %s", mem.ID)
		}
	}
}

func BenchmarkStoreSearch10K(b *testing.B) {
	ctx := context.Background()
	st := openBenchmarkStore(b)
	seedBenchmarkMemories(b, ctx, st, benchmarkMemoryCount)

	params := SearchParams{
		Query:     "sqlite retrieval benchmark latency",
		ScopeKind: "project",
		ScopeID:   "bench-project",
		Limit:     8,
		Lifecycle: LifecycleCurrent,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := st.Search(ctx, params)
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected benchmark search results")
		}
	}
}

func BenchmarkStoreWakeReads10K(b *testing.B) {
	ctx := context.Background()
	st := openBenchmarkStore(b)
	seedBenchmarkMemories(b, ctx, st, benchmarkMemoryCount)

	searchParams := SearchParams{
		Query:     "sqlite retrieval benchmark latency",
		ScopeKind: "project",
		ScopeID:   "bench-project",
		Limit:     20,
		Lifecycle: LifecycleCurrent,
	}
	listParams := ListParams{
		ScopeKind: "project",
		ScopeID:   "bench-project",
		Limit:     20,
		Lifecycle: LifecycleCurrent,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		queryResults, err := st.Search(ctx, searchParams)
		if err != nil {
			b.Fatal(err)
		}
		recent, err := st.List(ctx, listParams)
		if err != nil {
			b.Fatal(err)
		}
		if len(queryResults) == 0 || len(recent) == 0 {
			b.Fatalf("expected wake backing results, query=%d recent=%d", len(queryResults), len(recent))
		}
	}
}

func openBenchmarkStore(b *testing.B) *Store {
	b.Helper()
	st, err := Open(filepath.Join(b.TempDir(), "recoil-bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := st.Close(); err != nil {
			b.Fatal(err)
		}
	})
	return st
}

func seedBenchmarkMemories(b *testing.B, ctx context.Context, st *Store, count int) {
	b.Helper()
	for i := 0; i < count; i++ {
		validity := "active"
		if i%23 == 0 {
			validity = "rejected"
		} else if i%29 == 0 {
			validity = "stale"
		}
		_, _, err := st.AddMemory(ctx, benchmarkMemoryParams(i, validity))
		if err != nil {
			b.Fatalf("seed memory %d: %v", i, err)
		}
	}
}

func benchmarkMemoryParams(i int, validity string) AddMemoryParams {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	topic := benchmarkTopics[i%len(benchmarkTopics)]
	return AddMemoryParams{
		Role:        benchmarkRole(i),
		Content:     fmt.Sprintf("Benchmark memory %05d records %s for recoil sqlite retrieval benchmark latency and local FTS recall target_%03d.", i, topic, i%997),
		SourceAgent: "bench",
		SourcePath:  fmt.Sprintf("docs/bench/%02d.md", i%64),
		SourceRef:   fmt.Sprintf("chunk %d lines %d-%d", i%17, i%200, i%200+8),
		ScopeKind:   "project",
		ScopeID:     "bench-project",
		ProjectID:   "bench-project",
		Validity:    validity,
		CreatedAt:   base.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
	}
}

func benchmarkRole(i int) string {
	switch i % 11 {
	case 0:
		return "decision"
	case 1:
		return "adr"
	default:
		return "source"
	}
}

var benchmarkTopics = []string{
	"scope isolation",
	"sqlite driver",
	"wake layering",
	"metadata lifecycle",
	"project mining",
	"session recall",
	"user preference",
	"json envelope",
}
