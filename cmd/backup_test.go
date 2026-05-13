package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestBackupCreatesValidSnapshotAndRotates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:      "decision",
		Content:   "backup test memory",
		ScopeKind: "project",
		ScopeID:   "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 4; i++ {
		c := newBackupCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--max", "2"})
		if err := c.Execute(); err != nil {
			t.Fatalf("backup %d failed: %v", i, err)
		}
	}

	dir := filepath.Dir(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "recoil.db.bak.") {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups after rotation (max=2), got %d: %v", len(backups), backups)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	latest := backups[0]
	bst, err := store.Open(latest)
	if err != nil {
		t.Fatalf("opening backup %s: %v", latest, err)
	}
	defer bst.Close()
	got, err := bst.GetMemoryByID(context.Background(), mem.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Content != "backup test memory" {
		t.Fatalf("expected seeded memory inside backup, got %+v", got)
	}
}

func TestBackupHonorsOutDirAndSettingsDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	if st, err := store.Open(dbPath); err != nil {
		t.Fatal(err)
	} else {
		st.Close()
	}

	outDir := t.TempDir()
	c := newBackupCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--out", outDir})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "recoil.db.bak.") {
		t.Fatalf("expected one backup file in --out dir, got %+v", entries)
	}

	settingsDir := t.TempDir()
	cfg := newConfigCommand()
	cfg.SetOut(&bytes.Buffer{})
	cfg.SetErr(&bytes.Buffer{})
	cfg.SetArgs([]string{"set", "backup.dir", settingsDir})
	if err := cfg.Execute(); err != nil {
		t.Fatal(err)
	}

	c = newBackupCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(nil)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(settingsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "recoil.db.bak.") {
		t.Fatalf("expected backup in settings.backup.dir, got %+v", entries)
	}
}
