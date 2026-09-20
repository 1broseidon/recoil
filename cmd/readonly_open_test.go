package cmd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

// resetSchemaVersion rewinds user_version to 0 so a store looks like one written
// by a binary from before schema stamping existed.
func resetSchemaVersion(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
}

func runReadCommand(c *cobra.Command, args ...string) error {
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	return c.Execute()
}

func decodeMigrateResult(t *testing.T, raw []byte) migrateResult {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != "migrate_result" {
		t.Fatalf("expected migrate_result envelope, got %q", env.Kind)
	}
	data, _ := json.Marshal(env.Data)
	var result migrateResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func readOnlyOpenFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath, json: true}
	t.Cleanup(func() { opts = oldOpts })

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(t.Context(), store.AddMemoryParams{
		Role:      "decision",
		Content:   "Read paths must not take a write lock.",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  "store.readonly",
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func TestOpenReadStoreUsesAReadOnlyHandle(t *testing.T) {
	readOnlyOpenFixture(t)

	st, _, err := openReadStore()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.ReadOnly() {
		t.Fatal("expected openReadStore to return a read-only handle")
	}
	if st.Migrated() {
		t.Fatal("expected no migration on a read path")
	}
	if lastReadOnlyFallback.Reason != "" {
		t.Fatalf("did not expect a fallback, got %+v", lastReadOnlyFallback)
	}
}

func TestOpenReadStoreFallsBackAndMigratesOnVersionMismatch(t *testing.T) {
	dbPath := readOnlyOpenFixture(t)

	// Simulate a store last written by a binary that predates user_version.
	writable, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	resetSchemaVersion(t, dbPath)

	st, _, err := openReadStore()
	if err != nil {
		t.Fatal(err)
	}
	if st.ReadOnly() {
		t.Fatal("expected the fallback to hand back a writable handle")
	}
	if !st.Migrated() {
		t.Fatal("expected the fallback open to migrate the outdated store")
	}
	if !lastReadOnlyFallback.Schema {
		t.Fatalf("expected a schema-version fallback to be recorded, got %+v", lastReadOnlyFallback)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Now that it is stamped, the read path goes back to read-only.
	reopened, _, err := openReadStore()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.ReadOnly() {
		t.Fatal("expected read-only opens to resume after the one-time upgrade")
	}
}

func TestOpenReadStoreFallsBackWhenStoreDoesNotExistYet(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "fresh", "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	st, _, err := openReadStore()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.ReadOnly() {
		t.Fatal("a store that does not exist yet must fall back to a creating open")
	}
	if lastReadOnlyFallback.Schema {
		t.Fatalf("a missing store is not a schema mismatch, got %+v", lastReadOnlyFallback)
	}
}

func TestReadCommandsWorkOverAReadOnlyStore(t *testing.T) {
	readOnlyOpenFixture(t)

	// Each of these must succeed against a store opened without write rights.
	for name, run := range map[string]func() error{
		"list":   func() error { return runReadCommand(newListCommand()) },
		"claims": func() error { return runReadCommand(newClaimsCommand()) },
		"status": func() error { return runReadCommand(newStatusCommand()) },
		"export": func() error { return runReadCommand(newExportCommand(), "--claim-key", "store.readonly") },
		"search": func() error { return runReadCommand(newSearchCommand(), "write lock read paths") },
		"check":  func() error { return runReadCommand(newCheckCommand(), "--claim-key", "store.readonly") },
	} {
		if err := run(); err != nil {
			t.Fatalf("%s failed over a read-only store: %v", name, err)
		}
	}
}

func TestMigrateCommandReportsSchemaVersion(t *testing.T) {
	dbPath := readOnlyOpenFixture(t)
	resetSchemaVersion(t, dbPath)

	c := newMigrateCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	result := decodeMigrateResult(t, out.Bytes())
	if !result.Migrated {
		t.Fatalf("expected the outdated store to be migrated, got %+v", result)
	}
	if result.SchemaVersion != store.SchemaVersion {
		t.Fatalf("expected schema version %d, got %+v", store.SchemaVersion, result)
	}

	// Already current: nothing to do, and that is not an error.
	again := newMigrateCommand()
	var againOut bytes.Buffer
	again.SetOut(&againOut)
	again.SetErr(&bytes.Buffer{})
	if err := again.Execute(); err != nil {
		t.Fatal(err)
	}
	if decodeMigrateResult(t, againOut.Bytes()).Migrated {
		t.Fatal("expected a second migrate on a current store to be a no-op")
	}

	forced := newMigrateCommand()
	var forcedOut bytes.Buffer
	forced.SetOut(&forcedOut)
	forced.SetErr(&bytes.Buffer{})
	forced.SetArgs([]string{"--force"})
	if err := forced.Execute(); err != nil {
		t.Fatal(err)
	}
	result = decodeMigrateResult(t, forcedOut.Bytes())
	if !result.Forced || !result.Migrated {
		t.Fatalf("expected --force to re-run migrations, got %+v", result)
	}
}
