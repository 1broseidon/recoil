package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/store"
)

func TestSearchCommandSeparatesCurrentAndHistoricalResults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Pure Go sqlite was rejected because FTS5 support is required.",
		ScopeKind: "session",
		ScopeID:   "project-1",
		Validity:  "rejected",
		ClaimKey:  "dependency.sqlite-driver",
	})
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "adr",
		Content:   "Current sqlite driver is mattn go sqlite3 with FTS5.",
		ScopeKind: "session",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "dependency.sqlite-driver",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newSearchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "project-1", "sqlite FTS5"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"result_count: 1",
		"history_count: 1",
		"## Current Results",
		"## Historical Results",
		active.ID,
		"validity: rejected",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}
