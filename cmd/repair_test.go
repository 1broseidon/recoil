package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/store"
)

func TestRepairAgeDemotesOldHandoffAndExpiredPredicate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	oldHandoff, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "handoff", SourceKind: "direct", Content: "old handoff", ScopeKind: "session", ScopeID: "repair-age", Validity: "active", ClaimKey: "same", CreatedAt: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	newHandoff, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "handoff", SourceKind: "direct", Content: "new handoff", ScopeKind: "session", ScopeID: "repair-age", Validity: "active", ClaimKey: "same", CreatedAt: "2026-02-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "expired predicate", ScopeKind: "session", ScopeID: "repair-age", Validity: "active", MetadataJSON: `{"predicate":{"kind":"valid_until","valid_until":"2020-01-01"}}`})
	if err != nil {
		t.Fatal(err)
	}

	result, err := repairAge(ctx, st, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.HandoffsHistorical != 1 || result.PredicatesStale != 1 {
		t.Fatalf("unexpected repair summary: %+v", result)
	}
	oldAfter, err := st.GetMemoryByID(ctx, oldHandoff.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	newAfter, err := st.GetMemoryByID(ctx, newHandoff.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	expiredAfter, err := st.GetMemoryByID(ctx, expired.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if oldAfter.Validity != "superseded" || newAfter.Validity != "active" || expiredAfter.Validity != "stale" {
		t.Fatalf("unexpected validity after repair: old=%s new=%s expired=%s", oldAfter.Validity, newAfter.Validity, expiredAfter.Validity)
	}
	second, err := repairAge(ctx, st, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.HandoffsHistorical != 0 || second.PredicatesStale != 0 {
		t.Fatalf("expected second run no-op, got %+v", second)
	}
}
