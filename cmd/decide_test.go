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

func TestDecideCommandAddsActiveDecisionWithClaimKey(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	c := newDecideCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--agent", "codex", "--claim-key", "dependency.sqlite-driver", "Use mattn/go-sqlite3 with FTS5."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "validity: active") || !strings.Contains(out.String(), "claim_key: dependency.sqlite-driver") {
		t.Fatalf("expected active decision output, got:\n%s", out.String())
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
	results, err := st.List(context.Background(), store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		Role:      "decision",
		ClaimKey:  "dependency.sqlite-driver",
		Validity:  "active",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Role != "decision" || results[0].Validity != "active" {
		t.Fatalf("expected one active decision, got %+v", results)
	}
}

func TestDecideCommandRequiresClaimKey(t *testing.T) {
	c := newDecideCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"Missing claim key."})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "--claim-key is required") {
		t.Fatalf("expected claim-key error, got %v", err)
	}
}
