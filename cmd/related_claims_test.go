package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestRememberSurfacesRelatedClaim(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	first := newRememberCommand()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--role", "decision", "--claim-key", "dependency.cache.redis", "Use Redis for cache session storage with expiring keys."})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}

	second := newRememberCommand()
	var out bytes.Buffer
	second.SetOut(&out)
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--role", "decision", "Store cache sessions in Redis using TTL-backed keys."})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result rememberResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.RelatedClaims) == 0 || result.RelatedClaims[0].ClaimKey != "dependency.cache.redis" {
		t.Fatalf("expected related redis claim, got %+v", result.RelatedClaims)
	}
}

func TestRememberUnrelatedLeavesRelatedClaimsEmpty(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	first := newRememberCommand()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--role", "decision", "--claim-key", "dependency.cache.redis", "Use Redis for cache session storage with expiring keys."})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}

	second := newRememberCommand()
	var out bytes.Buffer
	second.SetOut(&out)
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--role", "decision", "Render markdown documentation with goldmark extensions."})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result rememberResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.RelatedClaims) != 0 {
		t.Fatalf("expected no related claims, got %+v", result.RelatedClaims)
	}
}

func TestDecideSurfacesPossibleConflicts(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	first := newDecideCommand()
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	first.SetArgs([]string{"--claim-key", "dependency.cache.redis", "Use Redis for cache session storage with expiring keys."})
	if err := first.Execute(); err != nil {
		t.Fatal(err)
	}

	second := newDecideCommand()
	var out bytes.Buffer
	second.SetOut(&out)
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{"--claim-key", "cache.session.backend", "Store cache sessions in Redis using TTL-backed keys."})
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result addResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.PossibleConflicts) == 0 || result.PossibleConflicts[0].ClaimKey != "dependency.cache.redis" {
		t.Fatalf("expected possible conflict, got %+v", result.PossibleConflicts)
	}
}

func TestClaimsCommandListsFamilySummaries(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	old, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "Use sqlite old.", ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Validity: "active"})
	if err != nil {
		t.Fatal(err)
	}
	newMem, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "Use postgres new.", ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "db.choice", Validity: "active", Supersedes: old.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpdateLifecycle(ctx, store.LifecycleParams{IDOrPrefix: old.ID, Validity: "superseded", ClaimKey: "db.choice", SupersededBy: newMem.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "Use redis one.", ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "cache.choice", Validity: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "Use redis two.", ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "cache.choice", Validity: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "Rejected queue.", ScopeKind: sc.Kind, ScopeID: sc.ID, ClaimKey: "queue.choice", Validity: "rejected"}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	c := newClaimsCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result claimsResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]store.ClaimSummary{}
	for _, summary := range result.Claims {
		byKey[summary.ClaimKey] = summary
	}
	if got := byKey["db.choice"]; got.Count != 2 || got.CurrentValidity != "active" || got.CurrentID != newMem.ID {
		t.Fatalf("unexpected db.choice summary: %+v", got)
	}
	if got := byKey["cache.choice"]; got.Count != 2 || got.CurrentValidity != "multiple" || got.CurrentID != "" {
		t.Fatalf("unexpected cache.choice summary: %+v", got)
	}
	if got := byKey["queue.choice"]; got.Count != 1 || got.CurrentValidity != "none" || got.CurrentID != "" {
		t.Fatalf("unexpected queue.choice summary: %+v", got)
	}
}
