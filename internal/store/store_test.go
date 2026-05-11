package store

import (
	"context"
	"database/sql"
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

func TestAddMemoryResurrectsTombstonedDuplicate(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, duplicate, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "A decision that can be intentionally re-added later.",
		ScopeKind: "project",
		ScopeID:   "project-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("first insert should not be a duplicate")
	}
	if _, err := st.ForgetMemory(ctx, ForgetParams{IDOrPrefix: mem.ID, Reason: "changed my mind"}); err != nil {
		t.Fatal(err)
	}

	resurrected, duplicate, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "A decision that can be intentionally re-added later.",
		ScopeKind: "project",
		ScopeID:   "project-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatal("resurrected insert should be reported as active, not duplicate")
	}
	if resurrected.ID != mem.ID {
		t.Fatalf("expected same memory ID after resurrection: %s != %s", resurrected.ID, mem.ID)
	}
	if resurrected.TombstonedAt != "" {
		t.Fatalf("expected tombstone cleared, got %q", resurrected.TombstonedAt)
	}

	results, err := st.Search(ctx, SearchParams{
		Query:     "intentionally re-added",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != mem.ID {
		t.Fatalf("expected resurrected memory in search, got %+v", results)
	}
}

func TestRetrievalScoreIncreasesWithStrongerNegativeRank(t *testing.T) {
	weak := retrievalScore(-0.1)
	strong := retrievalScore(-10)
	if weak <= 0 || weak >= 1 {
		t.Fatalf("expected weak score in (0,1), got %f", weak)
	}
	if strong <= weak || strong >= 1 {
		t.Fatalf("expected stronger rank to produce higher score below 1, weak=%f strong=%f", weak, strong)
	}
	if retrievalScore(0) != 0 {
		t.Fatalf("expected zero rank to produce zero score, got %f", retrievalScore(0))
	}
}

func TestAddMemoryPersistsLifecycleMetadata(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:      "Recoil uses mattn go sqlite3 for FTS5 support.",
		ScopeKind:    "project",
		ScopeID:      "project-1",
		Validity:     "active",
		ClaimKey:     "dependency.sqlite-driver",
		Supersedes:   "mem_old",
		SupersededBy: "mem_future",
		MetadataJSON: `{"fixture_id":"mem_sqlite_mattn"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validity != "active" ||
		got.ClaimKey != "dependency.sqlite-driver" ||
		got.Supersedes != "mem_old" ||
		got.SupersededBy != "mem_future" {
		t.Fatalf("unexpected lifecycle fields: %+v", got)
	}

	results, err := st.Search(ctx, SearchParams{
		Query:     "sqlite3 FTS5",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Validity != "active" || results[0].ClaimKey == "" {
		t.Fatalf("expected lifecycle fields in search result, got %+v", results)
	}
}

func TestAddMemoryExtractsLifecycleFromMetadataJSON(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mem, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:      "Pure Go sqlite was rejected because FTS5 is required.",
		ScopeKind:    "project",
		ScopeID:      "project-1",
		MetadataJSON: `{"validity":"rejected","claim_key":"dependency.sqlite-driver","superseded_by":"mem_sqlite_mattn"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mem.Validity != "rejected" ||
		mem.ClaimKey != "dependency.sqlite-driver" ||
		mem.SupersededBy != "mem_sqlite_mattn" {
		t.Fatalf("expected lifecycle fields from metadata, got %+v", mem)
	}
}

func TestUpdateLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	old, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:   "Earlier source plan made Brainfile required.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "source.brainfile",
	})
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := st.AddMemory(ctx, AddMemoryParams{
		Content:    "Current source plan keeps Brainfile optional.",
		ScopeKind:  "project",
		ScopeID:    "project-1",
		Validity:   "active",
		ClaimKey:   "source.brainfile",
		Supersedes: old.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := st.UpdateLifecycle(ctx, LifecycleParams{
		IDOrPrefix:   old.ID[:12],
		Validity:     "superseded",
		ClaimKey:     "source.brainfile",
		SupersededBy: newer.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Validity != "superseded" || updated.SupersededBy != newer.ID {
		t.Fatalf("expected superseded old memory, got %+v", updated)
	}
}

func TestAddMemoryRejectsInvalidValidity(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, _, err = st.AddMemory(ctx, AddMemoryParams{
		Content:   "Bad validity should not persist.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "currentish",
	})
	if err == nil || !strings.Contains(err.Error(), `invalid validity "currentish"`) {
		t.Fatalf("expected invalid validity error, got %v", err)
	}
}

func TestMigrationAddsLifecycleColumnsAndBackfillsMetadata(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE memories (
			pk INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE,
			hash TEXT NOT NULL UNIQUE,
			role TEXT,
			content TEXT NOT NULL,
			source_agent TEXT,
			source_path TEXT,
			source_ref TEXT,
			scope_kind TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			project_id TEXT,
			session_id TEXT,
			room TEXT,
			metadata_json TEXT,
			created_at TEXT NOT NULL,
			tombstoned_at TEXT,
			purge_reason TEXT
		);
		INSERT INTO memories (
			id, hash, role, content, scope_kind, scope_id, metadata_json, created_at
		) VALUES (
			'mem_legacy', 'legacy_hash', 'decision', 'Legacy active metadata row.',
			'project', 'project-1',
			'{"validity":"active","claim_key":"legacy.claim","superseded_by":"mem_new"}',
			'2026-01-01T00:00:00Z'
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetMemoryByID(ctx, "mem_legacy", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validity != "active" || got.ClaimKey != "legacy.claim" || got.SupersededBy != "mem_new" {
		t.Fatalf("expected migrated lifecycle metadata, got %+v", got)
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
