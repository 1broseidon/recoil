package cmd

import (
	"bytes"
	"context"
	"fmt"
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
		"## Current Decisions",
		"## Historical",
		"why: current adr with claim_key dependency.sqlite-driver",
		active.ID,
		"validity: rejected",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestSearchCommandCurrentResultsSurviveHistoricalCrowding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:      "decision",
			Content:   fmt.Sprintf("Crowded search phrase rejected history %02d repeated repeated repeated.", i),
			ScopeKind: "session",
			ScopeID:   "crowded-search",
			Validity:  "rejected",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "adr",
		Content:   "Crowded search phrase active canonical guidance.",
		ScopeKind: "session",
		ScopeID:   "crowded-search",
		Validity:  "active",
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
	c.SetArgs([]string{"--session", "crowded-search", "--limit", "5", "crowded search phrase"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"result_count: 1",
		"history_count: 5",
		active.ID,
		"## Current Decisions",
		"## Historical",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestRunSignalSearchFusesQueryVariants(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	lee, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy.",
		SourcePath: "sessions/health.jsonl",
		ScopeKind:  "session",
		ScopeID:    "query-variants",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	smith, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Dr. Smith prescribed antibiotics after the urinary tract infection.",
		SourcePath: "sessions/uti.jsonl",
		ScopeKind:  "session",
		ScopeID:    "query-variants",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Which doctor discussed biopsy and which doctor handled the UTI?",
		ScopeKind:    "session",
		ScopeID:      "query-variants",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !containsMemoryID(results, lee.ID) || !containsMemoryID(results, smith.ID) {
		t.Fatalf("expected variant fusion to retrieve both doctor memories, got %+v", results)
	}
}

func TestRunSignalSearchDiversifiesSources(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for i := range 3 {
		if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Content:    fmt.Sprintf("sqlite FTS5 search ranking canonical repeated note %d", i),
			SourcePath: "docs/main.md",
			ScopeKind:  "session",
			ScopeID:    "diversity",
			Validity:   "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	other, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "search ranking caveat from release notes",
		SourcePath: "docs/release.md",
		ScopeKind:  "session",
		ScopeID:    "diversity",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "sqlite FTS5 and search ranking",
		ScopeKind:    "session",
		ScopeID:      "diversity",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !containsMemoryID(results, other.ID) {
		t.Fatalf("expected diversified results to include second source, got %+v", results)
	}
}

func TestRunSignalSearchPrioritizesOperationalDocsForBroadAgentQueries(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	agentDoc, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "This file provides guidance to AI agents before editing this repository. Run focused tests and follow local conventions.",
		SourceKind: "file",
		SourcePath: "AGENTS.md",
		ScopeKind:  "session",
		ScopeID:    "source-priors",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "An example application README for agents before editing setup tests repository guidance.",
		SourceKind: "file",
		SourcePath: "examples/demo/README.md",
		ScopeKind:  "session",
		ScopeID:    "source-priors",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "what should agents know before editing this repo",
		ScopeKind:    "session",
		ScopeID:      "source-priors",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != agentDoc.ID {
		t.Fatalf("expected AGENTS.md first for broad agent query, got %+v", results)
	}
}

func TestSearchPrioritizesSecurityDisclosureDocs(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	securityDoc, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Security Policy. Reporting a vulnerability uses the private disclosure process.",
		SourceKind: "file",
		SourcePath: ".github/SECURITY.md",
		ScopeKind:  "session",
		ScopeID:    "security-priors",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Example app security policy rows for database authorization and vulnerability-shaped test content.",
		SourceKind: "file",
		SourcePath: "examples/auth/README.md",
		ScopeKind:  "session",
		ScopeID:    "security-priors",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := st.Search(ctx, store.SearchParams{
		Query:        "security policy vulnerability disclosure",
		ScopeKind:    "session",
		ScopeID:      "security-priors",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != securityDoc.ID {
		t.Fatalf("expected security policy first, got %+v", results)
	}
}

func TestRunSignalSearchHonorsExplicitDoctorName(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy.",
		SourcePath: "sessions/health.jsonl",
		ScopeKind:  "session",
		ScopeID:    "doctor-name",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Dr. Smith prescribed antibiotics after the urinary tract infection.",
		SourcePath: "sessions/uti.jsonl",
		ScopeKind:  "session",
		ScopeID:    "doctor-name",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Dr. Alvarez cardiologist appointment",
		ScopeKind:    "session",
		ScopeID:      "doctor-name",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no unrelated doctor results for explicit Dr. Alvarez query, got %+v", results)
	}
}

func TestRunSignalSearchExpandsDerivedTraceToSourceEvidence(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	raw, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "source",
		Content:      "user: I prefer deep learning for medical image analysis.",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/profile.jsonl",
		SourceRef:    "turn 1-2",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		SessionID:    "sess-profile",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	derived, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "preference",
		Content:      "Derived user profile trace: user_interest publications conferences deep medical image analysis",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/profile.jsonl",
		SourceRef:    "turn 1-2 profile",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		SessionID:    "sess-profile",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence","derived_type":"derived_profile"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "publications conferences medical imaging",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != derived.ID || results[1].ID != raw.ID {
		t.Fatalf("expected derived trace followed by raw parent evidence, got %+v", results)
	}
}

func TestRunSignalSearchExpandsSourceEvidenceToDerivedChildren(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	raw, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "source",
		Content:      "user: Switch to mattn/go-sqlite3 now; no longer use modernc.",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/update.jsonl",
		SourceRef:    "turn 5-6",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		SessionID:    "sess-update",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "decision",
		Content:      "Derived update trace: current_state latest_update sqlite mattn modernc",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/update.jsonl",
		SourceRef:    "turn 5-6 update",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		SessionID:    "sess-update",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence","derived_type":"derived_update"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Switch to mattn go sqlite3",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		Role:         "source",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != raw.ID || results[1].ID != child.ID {
		t.Fatalf("expected raw evidence followed by derived child trace, got %+v", results)
	}
}

func containsMemoryID(memories []store.Memory, id string) bool {
	for _, mem := range memories {
		if mem.ID == id {
			return true
		}
	}
	return false
}
