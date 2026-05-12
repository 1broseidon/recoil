package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestSessionEvidenceIngestMineAndForget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	transcript := filepath.Join(t.TempDir(), "transcript.json")
	writeCmdTestFile(t, transcript, `{
		"session_id": "sess-auth",
		"turns": [
			{"turn_index": 42, "role": "user", "content": "skip refresh tokens and use short bearer expiry for v0"},
			{"turn_index": 43, "role": "assistant", "content": "Understood. I will avoid refresh-token endpoints and keep bearer auth."}
		]
	}`)

	c := newSessionEvidenceCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"ingest", "--file", transcript, "--force", "--agent", "codex", "--session-id", "sess-auth"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "selected: 1") || !strings.Contains(out.String(), "mined=1") {
		t.Fatalf("unexpected ingest output:\n%s", out.String())
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
		Query:      "refresh tokens",
		ScopeKind:  sc.Kind,
		ScopeID:    sc.ID,
		SourceKind: "session_evidence",
		Limit:      5,
		Lifecycle:  store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceKind != "session_evidence" || results[0].SessionID != "sess-auth" {
		t.Fatalf("expected mined session evidence, got %+v", results)
	}
	if strings.Contains(results[0].Content, "tool") {
		t.Fatalf("did not expect tool payload in evidence content: %q", results[0].Content)
	}

	forget := newSessionEvidenceCommand()
	out.Reset()
	forget.SetOut(&out)
	forget.SetErr(&bytes.Buffer{})
	forget.SetArgs([]string{"forget", "sess-auth"})
	if err := forget.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "destroyed: true") {
		t.Fatalf("expected destroyed output, got:\n%s", out.String())
	}
	after, err := st.Search(context.Background(), store.SearchParams{
		Query:      "refresh tokens",
		ScopeKind:  sc.Kind,
		ScopeID:    sc.ID,
		SourceKind: "session_evidence",
		Limit:      5,
		Lifecycle:  store.LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("expected session evidence memories destroyed, got %+v", after)
	}
}

func TestSessionEvidenceIngestRequiresOptInUnlessForced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	transcript := filepath.Join(t.TempDir(), "transcript.json")
	writeCmdTestFile(t, transcript, `[{"role":"user","content":"go with bearer tokens for v0"}]`)

	c := newSessionEvidenceCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"ingest", "--file", transcript})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "session evidence is disabled") {
		t.Fatalf("expected opt-in error, got %v", err)
	}

	cfg := newConfigCommand()
	cfg.SetOut(&bytes.Buffer{})
	cfg.SetErr(&bytes.Buffer{})
	cfg.SetArgs([]string{"set", "session-evidence.enabled", "true"})
	if err := cfg.Execute(); err != nil {
		t.Fatal(err)
	}
	c = newSessionEvidenceCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"ingest", "--file", transcript, "--session-id", "sess-optin"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestMineSessionEvidenceCommandDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	transcript := filepath.Join(t.TempDir(), "transcript.json")
	writeCmdTestFile(t, transcript, `[{"turn_index":1,"role":"user","content":"go with CGO sqlite for FTS5"}]`)
	ingest := newSessionEvidenceCommand()
	ingest.SetOut(&bytes.Buffer{})
	ingest.SetErr(&bytes.Buffer{})
	ingest.SetArgs([]string{"ingest", "--file", transcript, "--force", "--session-id", "sess-mine", "--no-mine"})
	if err := ingest.Execute(); err != nil {
		t.Fatal(err)
	}

	c := newMineCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"session-evidence", "--dry-run"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would-add") || !strings.Contains(out.String(), "session-evidence/") {
		t.Fatalf("expected dry-run session evidence mine output, got:\n%s", out.String())
	}
}
