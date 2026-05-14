package cmd

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestDecideCommandStoresOptionalPredicateMetadata(t *testing.T) {
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
	c.SetArgs([]string{
		"--claim-key", "dependency.cache.redis",
		"--validity", "rejected",
		"--holds-while", "ops cost remains unjustified at current scale",
		"--recheck", "Has scale or cost picture changed?",
		"Decided against Redis for cache.",
	})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"validity: rejected", "claim_key: dependency.cache.redis", "predicate_status: unknown"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
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
	memories, err := st.List(context.Background(), store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  "dependency.cache.redis",
		Limit:     1,
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one decision, got %+v", memories)
	}
	pred, ok := predicateFromMemory(memories[0])
	if !ok {
		t.Fatalf("expected predicate metadata in %+v", memories[0])
	}
	if pred.Tier != "semantic" || pred.HoldsWhile == "" || pred.RecheckPrompt == "" {
		t.Fatalf("unexpected predicate: %+v", pred)
	}
}

func TestDecideCommandStoresStanceMetadata(t *testing.T) {
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
	c.SetArgs([]string{
		"--claim-key", "dependency.cache.redis",
		"--stance", "rejects",
		"--subject", "Redis",
		"Reject Redis for cache.",
	})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"stance: rejects", "subject: Redis"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
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
	memories, err := st.List(context.Background(), store.ListParams{
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  "dependency.cache.redis",
		Limit:     1,
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one decision, got %+v", memories)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(memories[0].MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["stance"] != "rejects" || metadata["subject"] != "Redis" {
		t.Fatalf("unexpected decision metadata: %s", memories[0].MetadataJSON)
	}
}
