package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestMineCommandAddsGenericProjectFiles(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "README.md"), "# Project\n\nAlpha mining decision lives here.")
	writeCmdTestFile(t, filepath.Join(root, ".brainfile", "board", "task.md"), "Hidden Brainfile task should not be mined.")
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	c := newMineCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "added: 1") {
		t.Fatalf("expected one added chunk, got:\n%s", out.String())
	}

	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := st.Search(context.Background(), store.SearchParams{
		Query:     "Alpha mining decision",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourcePath != "README.md" {
		t.Fatalf("expected README result, got %+v", results)
	}
	hidden, err := st.Search(context.Background(), store.SearchParams{
		Query:     "Hidden Brainfile task",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("expected hidden Brainfile file to be skipped, got %+v", hidden)
	}
}

func writeCmdTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
