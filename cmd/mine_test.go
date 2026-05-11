package cmd

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestMineCommandJSONUsesDataEnvelope(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "README.md"), "# Project\n\nAlpha mining decision lives here.")
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{json: true}
	defer func() { opts = oldOpts }()

	c := newMineCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--dry-run"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope["kind"]) != `"mine_result"` {
		t.Fatalf("expected mine_result kind, got:\n%s", out.String())
	}
	if _, ok := envelope["results"]; ok {
		t.Fatalf("did not expect legacy top-level results field, got:\n%s", out.String())
	}

	var data struct {
		Chunks  int               `json:"chunks"`
		Results []mineChunkResult `json:"results"`
	}
	if err := json.Unmarshal(envelope["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Chunks != 1 || len(data.Results) != 1 || data.Results[0].SourcePath != "README.md" {
		t.Fatalf("expected one README.md result in data, got %+v", data)
	}
}

func TestMineCommandMarksChangedSourceChunksStale(t *testing.T) {
	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	writeCmdTestFile(t, readme, "# Project\n\nAlpha mining decision lives here.")
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	runMineCommand(t)
	writeCmdTestFile(t, readme, "# Project\n\nBeta mining decision replaced it.")
	runMineCommand(t)

	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := st.Search(context.Background(), store.SearchParams{
		Query:     "Alpha",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     5,
		Lifecycle: store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 0 {
		t.Fatalf("expected changed old chunk to be stale, got current results %+v", alpha)
	}
	beta, err := st.Search(context.Background(), store.SearchParams{
		Query:     "Beta mining decision",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Limit:     5,
		Lifecycle: store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(beta) != 1 || beta[0].SourcePath != "README.md" {
		t.Fatalf("expected replacement README chunk current, got %+v", beta)
	}
}

func runMineCommand(t *testing.T) string {
	t.Helper()
	c := newMineCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
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
