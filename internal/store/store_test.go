package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSearchAndGetMemory(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, duplicate, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "We changed auth token storage to the keyring after a local persistence review.",
		ScopeKind: "project",
		ScopeID:   "project-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("first insert should not be a duplicate")
	}

	again, duplicate, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "We changed auth token storage to the keyring after a local persistence review.",
		ScopeKind: "project",
		ScopeID:   "project-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate {
		t.Fatal("second insert should be a duplicate")
	}
	if again.ID != mem.ID {
		t.Fatalf("duplicate returned different ID: %s != %s", again.ID, mem.ID)
	}

	results, err := st.Search(ctx, SearchParams{
		Query:     "auth keyring",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != mem.ID {
		t.Fatalf("search returned wrong memory: %s != %s", results[0].ID, mem.ID)
	}

	got, err := st.GetMemory(ctx, mem.ID[:12])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Content, "keyring") {
		t.Fatalf("expected retrieved content to mention keyring, got %q", got.Content)
	}
}

func TestAddMemoryRedactsDeterministically(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz",
		ScopeKind: "user",
		ScopeID:   "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mem.Content, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret was not redacted: %q", mem.Content)
	}
}

func TestListFiltersByScopeAndAgent(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, _, err = st.AddMemory(ctx, AddMemoryParams{
		Content:     "Project alpha decision from Codex.",
		SourceAgent: "codex",
		ScopeKind:   "project",
		ScopeID:     "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(ctx, AddMemoryParams{
		Content:     "Project alpha decision from Claude.",
		SourceAgent: "claude-code",
		ScopeKind:   "project",
		ScopeID:     "alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(ctx, AddMemoryParams{
		Content:     "Project beta decision from Codex.",
		SourceAgent: "codex",
		ScopeKind:   "project",
		ScopeID:     "beta",
	})
	if err != nil {
		t.Fatal(err)
	}

	memories, err := st.List(ctx, ListParams{
		ScopeKind:   "project",
		ScopeID:     "alpha",
		SourceAgent: "codex",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 scoped/agent-filtered memory, got %d", len(memories))
	}
	if memories[0].ScopeID != "alpha" || memories[0].SourceAgent != "codex" {
		t.Fatalf("unexpected memory: %+v", memories[0])
	}
}

func TestForgetTombstoneAndDestroy(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "A memory that should disappear from search after tombstone.",
		ScopeKind: "project",
		ScopeID:   "project-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ForgetMemory(ctx, ForgetParams{IDOrPrefix: mem.ID[:12], Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	results, err := st.Search(ctx, SearchParams{
		Query:     "disappear tombstone",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected tombstoned memory to disappear from search, got %d results", len(results))
	}
	listed, err := st.List(ctx, ListParams{
		ScopeKind:      "project",
		ScopeID:        "project-1",
		IncludeDeleted: true,
		Limit:          5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].TombstonedAt == "" {
		t.Fatalf("expected tombstoned memory in include-deleted list, got %+v", listed)
	}
	if _, err := st.ForgetMemory(ctx, ForgetParams{IDOrPrefix: mem.ID, Destroy: true}); err != nil {
		t.Fatal(err)
	}
	listed, err = st.List(ctx, ListParams{
		ScopeKind:      "project",
		ScopeID:        "project-1",
		IncludeDeleted: true,
		Limit:          5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected destroyed memory to be gone, got %+v", listed)
	}
}
