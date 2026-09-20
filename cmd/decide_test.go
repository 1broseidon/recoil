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

func TestDecideCommandAutoSupersedesPreviousDecision(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	for _, text := range []string{"Use sqlite.", "Use postgres."} {
		c := newDecideCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--claim-key", "db.choice", text})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
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
	family, err := st.List(context.Background(), store.ListParams{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Limit: 10, Lifecycle: store.LifecycleAny})
	if err != nil {
		t.Fatal(err)
	}
	if len(family) != 2 {
		t.Fatalf("expected two family memories, got %+v", family)
	}
	var current, old store.Memory
	for _, mem := range family {
		if mem.Validity == "active" {
			current = mem
		} else if mem.Validity == "superseded" {
			old = mem
		}
	}
	if current.ID == "" || old.ID == "" || old.SupersededBy != current.ID || current.Supersedes != old.ID {
		t.Fatalf("unexpected supersession links: current=%+v old=%+v family=%+v", current, old, family)
	}
	check, err := runCheck(context.Background(), st, sc, "", "db.choice", 8)
	if err != nil {
		t.Fatal(err)
	}
	if check.Verdict != "use" {
		t.Fatalf("expected check verdict use, got %+v", check)
	}
}

func TestDecideCommandNoSupersedeLeavesBothActive(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	for _, text := range []string{"Use sqlite.", "Use postgres."} {
		c := newDecideCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--claim-key", "db.choice", "--no-supersede", text})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
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
	current, err := st.List(context.Background(), store.ListParams{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Limit: 10, Lifecycle: store.LifecycleCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 {
		t.Fatalf("expected two current decisions, got %+v", current)
	}
}

func TestDecideCommandExplicitSupersedesSkipsAutoSupersede(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	first := newDecideCommand()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--claim-key", "db.choice", "Use sqlite."})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
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
	memories, err := st.List(context.Background(), store.ListParams{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Limit: 1, Lifecycle: store.LifecycleAny})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected first memory, got %+v", memories)
	}

	second := newDecideCommand()
	second.SetOut(&bytes.Buffer{})
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--claim-key", "db.choice", "--supersedes", memories[0].ID, "Use postgres."})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	current, err := st.List(context.Background(), store.ListParams{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Limit: 10, Lifecycle: store.LifecycleCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 {
		t.Fatalf("expected explicit supersedes to leave both current, got %+v", current)
	}
}

func TestDecideCommandDuplicateDoesNotAutoSupersede(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	for range []int{0, 1} {
		c := newDecideCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--claim-key", "db.choice", "Use sqlite."})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
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
	current, err := st.List(context.Background(), store.ListParams{ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Limit: 10, Lifecycle: store.LifecycleCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].SupersededBy != "" || current[0].Supersedes != "" {
		t.Fatalf("expected duplicate to leave one untouched current memory, got %+v", current)
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
