package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenStampsSchemaVersionAndSecondOpenSkipsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recoil.db")

	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Migrated() {
		t.Fatal("expected the first open of a fresh store to migrate")
	}
	version, err := first.schemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("expected user_version %d after migrate, got %d", SchemaVersion, version)
	}
	addClaim(t, first, "scope-a", "doctrine.one", "First claim.")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// The whole point of the user_version gate: a second open must not re-run
	// the DDL, the backfills, or the unconditional FTS rebuild.
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Migrated() {
		t.Fatal("expected the second open of an up-to-date store to skip migration entirely")
	}
	// Skipping migration must not cost correctness: the index still answers.
	results, err := second.Search(context.Background(), SearchParams{
		Query:     "doctrine first claim",
		ScopeKind: "project",
		ScopeID:   "scope-a",
		Limit:     5,
		Lifecycle: LifecycleAny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected the existing FTS index to still be searchable without a rebuild")
	}
}

func TestMigrateRunsAgainWhenSchemaVersionIsReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recoil.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if !second.Migrated() {
		t.Fatal("expected a store stamped at version 0 to migrate again")
	}
}

func TestForceMigrateReRunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recoil.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ForceMigrate(); err != nil {
		t.Fatal(err)
	}
	if !st.Migrated() {
		t.Fatal("expected ForceMigrate to migrate")
	}
	version, err := st.schemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("expected ForceMigrate to restamp version %d, got %d", SchemaVersion, version)
	}
}

func TestOpenReadOnlyReadsWithoutMigratingOrWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recoil.db")
	writable, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	addClaim(t, writable, "scope-ro", "doctrine.ro", "Readable without a write lock.")
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.ReadOnly() {
		t.Fatal("expected ReadOnly to report true")
	}
	if st.Migrated() {
		t.Fatal("a read-only open must never migrate")
	}
	memories, err := st.List(context.Background(), ListParams{
		ScopeKind: "project",
		ScopeID:   "scope-ro",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory through the read-only handle, got %d", len(memories))
	}

	// Writes must be refused by SQLite itself, not merely by convention.
	if _, _, err := st.AddMemory(context.Background(), AddMemoryParams{
		Role:      "decision",
		Content:   "Should never land.",
		ScopeKind: "project",
		ScopeID:   "scope-ro",
	}); err == nil {
		t.Fatal("expected a write through a read-only handle to fail")
	}
	if err := st.ForceMigrate(); err == nil {
		t.Fatal("expected ForceMigrate on a read-only handle to fail")
	}
}

func TestOpenReadOnlyRejectsOutdatedSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recoil.db")
	writable, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writable.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenReadOnly(path)
	if err == nil {
		t.Fatal("expected a schema version mismatch to be reported")
	}
	var schemaErr *SchemaVersionError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected *SchemaVersionError, got %T: %v", err, err)
	}
	if schemaErr.Got != 0 || schemaErr.Want != SchemaVersion {
		t.Fatalf("unexpected version detail: %+v", schemaErr)
	}

	// Open (which migrates) is the documented remedy and must fix it.
	repaired, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := repaired.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("expected read-only open to succeed after a migrating open: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenReadOnlyRefusesAMissingStore(t *testing.T) {
	if _, err := OpenReadOnly(filepath.Join(t.TempDir(), "absent.db")); err == nil {
		t.Fatal("expected OpenReadOnly to refuse a store that does not exist")
	}
}
