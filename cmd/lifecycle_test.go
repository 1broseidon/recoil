package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/store"
)

func TestMarkCommandUpdatesLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:     "Pure Go sqlite driver was evaluated earlier.",
		ScopeKind:   "project",
		ScopeID:     "project-1",
		Validity:    "active",
		ClaimKey:    "dependency.sqlite-driver",
		Supersedes:  "mem_older",
		SourceAgent: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newMarkCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{mem.ID[:12], "--validity", "rejected", "--superseded-by", "mem_new"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"action: mark",
		"validity: rejected",
		"claim_key: dependency.sqlite-driver",
		"supersedes: mem_older",
		"superseded_by: mem_new",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}

	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	updated, err := st.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Validity != "rejected" ||
		updated.ClaimKey != "dependency.sqlite-driver" ||
		updated.Supersedes != "mem_older" ||
		updated.SupersededBy != "mem_new" {
		t.Fatalf("unexpected marked memory: %+v", updated)
	}
}

func TestMarkCommandBackfillsDecisionStance(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Reject Redis for cache.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "dependency.cache.redis",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newMarkCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{mem.ID, "--stance", "rejects", "--subject", "Redis"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"stance: rejects", "subject: Redis"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}

	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	updated, err := st.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := decisionMetadataFromMemory(*updated)
	if meta.Stance != "rejects" || meta.Subject != "Redis" {
		t.Fatalf("unexpected decision metadata: %+v / %s", meta, updated.MetadataJSON)
	}
}

func TestSupersedeCommandCreatesReplacementAndMarksOld(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	oldMem, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Brainfile is required as the first project source.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "source.brainfile",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newSupersedeCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{oldMem.ID, "--agent", "codex", "Brainfile is optional; ordinary docs and direct memories are first-class sources."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"action: supersede",
		"claim_key: source.brainfile",
		"old_validity: superseded",
		"new_validity: active",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}

	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	updatedOld, err := st.GetMemory(ctx, oldMem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOld.Validity != "superseded" || updatedOld.SupersededBy == "" {
		t.Fatalf("expected old memory to be superseded, got %+v", updatedOld)
	}
	results, err := st.Search(ctx, store.SearchParams{
		Query:     "ordinary docs direct memories",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected replacement search result, got %+v", results)
	}
	replacement := results[0]
	if replacement.Validity != "active" ||
		replacement.ClaimKey != "source.brainfile" ||
		replacement.Supersedes != oldMem.ID ||
		updatedOld.SupersededBy != replacement.ID {
		t.Fatalf("unexpected supersession link old=%+v new=%+v", updatedOld, replacement)
	}
}
