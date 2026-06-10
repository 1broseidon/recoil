package traystats

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

func TestCollectScopedMemoryCounts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Current project guidance.",
		ScopeKind: "project",
		ScopeID:   "demo",
		Validity:  "active",
		CreatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Stale project guidance.",
		ScopeKind: "project",
		ScopeID:   "demo",
		Validity:  "stale",
		CreatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	tombstoned, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Forgotten project guidance.",
		ScopeKind: "project",
		ScopeID:   "demo",
		Validity:  "active",
		CreatedAt: now.Add(-4 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ForgetMemory(ctx, store.ForgetParams{IDOrPrefix: tombstoned.ID, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Other project guidance.",
		ScopeKind: "project",
		ScopeID:   "other",
		Validity:  "active",
		CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Collect(ctx, Target{
		DBPath:    dbPath,
		ScopeKind: "project",
		ScopeID:   "demo",
	}, CollectOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Ready {
		t.Fatalf("expected ready snapshot, got %#v", snapshot)
	}
	if snapshot.TotalCount != 3 || snapshot.ActiveCount != 2 || snapshot.CurrentCount != 1 {
		t.Fatalf("unexpected counts: %#v", snapshot)
	}
	if snapshot.StaleCount != 1 || snapshot.TombstoneCount != 1 || snapshot.Added24h != 2 {
		t.Fatalf("unexpected lifecycle counts: %#v", snapshot)
	}
	if snapshot.LastActivityAt == "" {
		t.Fatalf("expected last activity timestamp: %#v", snapshot)
	}
}

func TestCollectMissingDatabaseIsNotReady(t *testing.T) {
	snapshot, err := Collect(context.Background(), Target{
		DBPath: filepath.Join(t.TempDir(), "missing.db"),
	}, CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ready {
		t.Fatalf("expected missing database to be not ready: %#v", snapshot)
	}
	if snapshot.Message != ErrMissingDB.Error() {
		t.Fatalf("expected missing db message, got %q", snapshot.Message)
	}
}
