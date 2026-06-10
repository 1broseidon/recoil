package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestWakeCommandExplainIncludesScoreComponents(t *testing.T) {
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
		Role:       "decision",
		Content:    "Wake explain target keeps current context visible.",
		SourceKind: "direct",
		ScopeKind:  "session",
		ScopeID:    "explain-wake",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newWakeCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "explain-wake", "--explain", "--limit", "4"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"explain:", "final_score", "retrieval_mode", "guidance_role_prior"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in wake explain output:\n%s", want, got)
		}
	}
}

func TestBuildWakeLayersPrioritizesContextAndDecisions(t *testing.T) {
	recent := []store.Memory{
		{
			ID:        "mem_recent",
			Role:      "source",
			Content:   "A generic recently mined source chunk.",
			CreatedAt: "2026-05-11T03:00:00Z",
		},
		{
			ID:        "mem_decision",
			Role:      "decision",
			Content:   "Use github.com/mattn/go-sqlite3 with FTS5.",
			CreatedAt: "2026-05-11T02:00:00Z",
		},
		{
			ID:         "mem_handoff",
			Role:       "note",
			SourcePath: "HANDOFF.md",
			Content:    "Next active task is layered wake.",
			CreatedAt:  "2026-05-11T01:00:00Z",
		},
	}

	layers := buildWakeLayers("", nil, recent, 3)
	flat := flattenWakeLayers(layers)
	got := ids(flat)
	want := "mem_decision,mem_recent,mem_handoff"
	if got != want {
		t.Fatalf("unexpected wake order: got %s, want %s", got, want)
	}
	if layers[0].Key != "current_decisions" || len(layers[0].Memories) != 1 {
		t.Fatalf("expected decision in Current Decisions, got %+v", layers[0])
	}
	if layers[3].Key != "recent_evidence" || ids(layers[3].Memories) != "mem_recent,mem_handoff" {
		t.Fatalf("expected handoff in Recent Evidence, got %+v", layers[3])
	}
}

func TestBuildWakeLayersQuotasPreventDocCrowding(t *testing.T) {
	var recent []store.Memory
	for i := range 10 {
		recent = append(recent, store.Memory{
			ID:         fmt.Sprintf("doc_%02d", i),
			Role:       "source",
			SourceKind: "file",
			SourcePath: fmt.Sprintf("docs/%02d.md", i),
			Content:    "project doc chunk",
			CreatedAt:  fmt.Sprintf("2026-03-01T00:%02d:00Z", i),
			Score:      5,
		})
	}
	recent = append(recent,
		store.Memory{ID: "direct_handoff", Role: "handoff", SourceKind: "direct", Content: "new handoff", CreatedAt: "2026-03-02T00:00:00Z"},
		store.Memory{ID: "decision_1", Role: "decision", Content: "first decision", CreatedAt: "2026-03-02T00:01:00Z"},
		store.Memory{ID: "decision_2", Role: "decision", Content: "second decision", CreatedAt: "2026-03-02T00:02:00Z"},
	)

	layers := buildWakeLayers("", nil, recent, 8)
	flatIDs := ids(flattenWakeLayers(layers))
	for _, want := range []string{"direct_handoff", "decision_1", "decision_2"} {
		if !strings.Contains(flatIDs, want) {
			t.Fatalf("expected %s to survive doc crowding, got %s", want, flatIDs)
		}
	}
	if got := len(layers[2].Memories); got > 4 {
		t.Fatalf("expected project_docs count <= 4, got %d (%s)", got, ids(layers[2].Memories))
	}
}

func TestBuildWakeLayersKeepsOnlyNewestDirectHandoff(t *testing.T) {
	recent := []store.Memory{
		{ID: "old_handoff", Role: "handoff", SourceKind: "direct", ClaimKey: "a", Content: "old handoff", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "new_handoff", Role: "handoff", SourceKind: "direct", ClaimKey: "b", Content: "new handoff", CreatedAt: "2026-02-01T00:00:00Z"},
		{ID: "file_handoff", Role: "handoff", SourceKind: "file", Content: "file handoff chunk", CreatedAt: "2026-01-15T00:00:00Z"},
	}
	got := ids(flattenWakeLayers(buildWakeLayers("", nil, recent, 5)))
	if strings.Contains(got, "old_handoff") || !strings.Contains(got, "new_handoff") || !strings.Contains(got, "file_handoff") {
		t.Fatalf("expected only newest direct handoff plus file chunk, got %s", got)
	}
}

func TestBuildWakeLayersSkipsExpiredValidUntil(t *testing.T) {
	recent := []store.Memory{
		{ID: "expired", Role: "decision", Validity: "active", Content: "expired decision", MetadataJSON: `{"predicate":{"kind":"valid_until","valid_until":"2020-01-01","recheck_prompt":"Ask again"}}`},
		{ID: "future", Role: "decision", Validity: "active", Content: "future decision", MetadataJSON: `{"predicate":{"kind":"valid_until","valid_until":"2999-01-01"}}`},
	}
	got := ids(flattenWakeLayers(buildWakeLayers("", nil, recent, 5)))
	if strings.Contains(got, "expired") || !strings.Contains(got, "future") {
		t.Fatalf("expected expired predicate excluded and future included, got %s", got)
	}
}

func TestWakeCommandCurrentResultsSurviveStaleRecencyCrowding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Older active wake guidance should survive stale recency crowding.",
		ScopeKind: "session",
		ScopeID:   "crowded-wake",
		Validity:  "active",
		CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:      "note",
			Content:   fmt.Sprintf("Newer stale wake evidence %02d.", i),
			ScopeKind: "session",
			ScopeID:   "crowded-wake",
			Validity:  "stale",
			CreatedAt: fmt.Sprintf("2026-01-02T00:%02d:00Z", i),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newWakeCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "crowded-wake", "--minimal", "--limit", "5"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, active.ID) {
		t.Fatalf("expected active memory in wake output:\n%s", got)
	}
	if strings.Contains(got, "stale wake evidence") {
		t.Fatalf("did not expect stale memories in wake output:\n%s", got)
	}
}

func TestWakeCommandIncludesDecisionTrail(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Choose path A over path B because constraint C dominates.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "product.path",
		MetadataJSON: `{"predicate":{"tier":"semantic","kind":"semantic","recheck_prompt":"Has constraint C changed?"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newWakeCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--include-decisions", "--limit", "4"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"## Decision Trail",
		"### product.path",
		"predicate_status: unknown",
		"recheck: Has constraint C changed?",
		"decision: Choose path A over path B",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in wake output:\n%s", want, got)
		}
	}
}

func TestBuildWakeLayersPromotesQueryMatchesAndDedupes(t *testing.T) {
	match := store.Memory{ID: "mem_match", Role: "source", Content: "SQLite FTS evidence."}
	recent := []store.Memory{
		match,
		{ID: "mem_recent", Role: "note", Content: "Recent note."},
	}
	layers := buildWakeLayers("sqlite", []store.Memory{match}, recent, 3)
	flat := flattenWakeLayers(layers)
	got := ids(flat)
	want := "mem_match,mem_recent"
	if got != want {
		t.Fatalf("unexpected query wake order: got %s, want %s", got, want)
	}
	if len(layers[3].Memories) == 0 || layers[3].Memories[0].ID != "mem_match" {
		t.Fatalf("expected query match in Recent Evidence, got %+v", layers[3])
	}
}

func TestBuildWakeLayersSkipsHistoricalMemories(t *testing.T) {
	queryMatch := store.Memory{ID: "mem_rejected", Validity: "rejected", Content: "Old SQLite decision."}
	recent := []store.Memory{
		{ID: "mem_superseded", Validity: "superseded", Content: "Brainfile used to be required."},
		{ID: "mem_active", Validity: "active", Role: "decision", Content: "Brainfile is optional."},
		{ID: "mem_unknown", Content: "Unknown validity remains eligible."},
	}
	layers := buildWakeLayers("sqlite", []store.Memory{queryMatch}, recent, 5)
	got := ids(flattenWakeLayers(layers))
	want := "mem_active,mem_unknown"
	if got != want {
		t.Fatalf("unexpected wake memories: got %s, want %s", got, want)
	}
}

func TestBuildWakeLayersCapsSessionEvidence(t *testing.T) {
	recent := []store.Memory{
		{ID: "mem_handoff_evidence", SourceKind: "session_evidence", Content: "next step is to finish auth evidence."},
		{ID: "mem_must_not_l1", SourceKind: "session_evidence", Content: "user: must avoid refresh tokens for v0."},
		{ID: "mem_evidence_2", SourceKind: "session_evidence", Content: "user: go with bearer auth."},
		{ID: "mem_evidence_3", SourceKind: "session_evidence", Content: "user: prefer no refresh token endpoint."},
		{ID: "mem_doc", SourceKind: "file", Role: "source", Content: "docs say auth uses bearer tokens."},
	}
	layers := buildWakeLayers("", nil, recent, 8)
	if got := ids(layers[3].Memories); !strings.Contains(got, "mem_handoff_evidence") {
		t.Fatalf("expected session evidence in Recent Evidence, got %s", got)
	}
	if strings.Contains(ids(layers[0].Memories), "mem_must_not_l1") {
		t.Fatalf("did not expect session evidence promoted into Current Decisions, got %+v", layers[0].Memories)
	}
	recentIDs := ids(layers[3].Memories)
	if strings.Count(recentIDs, "mem_evidence") > 2 || strings.Contains(recentIDs, "mem_evidence_3") {
		t.Fatalf("expected Recent Evidence session evidence cap of two, got %s", recentIDs)
	}
	docIDs := ids(layers[2].Memories)
	if !strings.Contains(docIDs, "mem_doc") {
		t.Fatalf("expected ordinary file evidence to remain eligible, got %s", docIDs)
	}
}

func TestLayeredMemoryBlocksHonorsHardBudget(t *testing.T) {
	layers := []wakeLayer{
		{
			Key:   "l0_current_context",
			Title: "L0 Current Context",
			Memories: []store.Memory{
				{
					ID:         "mem_long",
					Role:       "note",
					SourcePath: "HANDOFF.md",
					SourceRef:  "chunk 1 lines 1-99",
					CreatedAt:  "2026-05-11T03:00:00Z",
					Content:    strings.Repeat("long context ", 80),
				},
			},
		},
	}
	rendered := layeredMemoryBlocks(layers, 220, true)
	body := rendered.Body
	if len(body) > 220 {
		t.Fatalf("wake body exceeded hard budget: %d\n%s", len(body), body)
	}
	if rendered.ShownCount != 1 {
		t.Fatalf("expected one shown memory, got %d", rendered.ShownCount)
	}
	if !rendered.Truncated {
		t.Fatal("expected truncated render")
	}
	if !strings.Contains(body, "L0 Current Context") {
		t.Fatalf("expected section heading, got:\n%s", body)
	}
	if !strings.Contains(body, "source_path: HANDOFF.md") {
		t.Fatalf("expected source metadata, got:\n%s", body)
	}
}

func TestLayeredMemoryBlocksPerMemoryCap(t *testing.T) {
	layers := []wakeLayer{
		{
			Key:   "recent_evidence",
			Title: "Recent Evidence",
			Memories: []store.Memory{
				{ID: "mem_1", CreatedAt: "2026-05-11T03:00:00Z", Content: strings.Repeat("a", 2000)},
				{ID: "mem_2", CreatedAt: "2026-05-11T03:01:00Z", Content: strings.Repeat("b", 2000)},
				{ID: "mem_3", CreatedAt: "2026-05-11T03:02:00Z", Content: strings.Repeat("c", 2000)},
				{ID: "mem_4", CreatedAt: "2026-05-11T03:03:00Z", Content: strings.Repeat("d", 2000)},
			},
		},
	}
	rendered := layeredMemoryBlocks(layers, 1600, true)
	if rendered.ShownCount < 3 {
		t.Fatalf("expected at least 3 memories rendered, got %d:\n%s", rendered.ShownCount, rendered.Body)
	}
	if len(rendered.Body) > 1600 {
		t.Fatalf("wake body exceeded hard budget: %d", len(rendered.Body))
	}
	if !rendered.Truncated {
		t.Fatal("expected per-memory truncation")
	}
}

func TestLayeredMemoryBlocksReportsOmittedMemories(t *testing.T) {
	layers := []wakeLayer{
		{
			Key:   "l2_recent_evidence",
			Title: "L2 Recent Notes And Evidence",
			Memories: []store.Memory{
				{
					ID:        "mem_first",
					CreatedAt: "2026-05-11T03:00:00Z",
					Content:   strings.Repeat("first ", 80),
				},
				{
					ID:        "mem_second",
					CreatedAt: "2026-05-11T03:01:00Z",
					Content:   "second memory should be omitted",
				},
			},
		},
	}
	rendered := layeredMemoryBlocks(layers, 180, true)
	if !rendered.Truncated {
		t.Fatal("expected truncation when selected memories are omitted")
	}
	if rendered.ShownCount != 1 {
		t.Fatalf("expected only the first memory to be shown, got %d", rendered.ShownCount)
	}
	if strings.Contains(rendered.Body, "mem_second") {
		t.Fatalf("expected second memory to be omitted, got:\n%s", rendered.Body)
	}
}

func TestBoundedOutputIsUTF8Safe(t *testing.T) {
	layers := []wakeLayer{
		{
			Key:   "l0_current_context",
			Title: "L0 Current Context",
			Memories: []store.Memory{
				{
					ID:        "mem_unicode",
					CreatedAt: "2026-05-11T03:00:00Z",
					Content:   strings.Repeat("é", 40),
				},
			},
		},
	}
	rendered := layeredMemoryBlocks(layers, 145, true)
	if !utf8.ValidString(rendered.Body) {
		t.Fatalf("expected valid UTF-8, got %q", rendered.Body)
	}
	if len(rendered.Body) > 145 {
		t.Fatalf("wake body exceeded hard budget: %d", len(rendered.Body))
	}
	if !rendered.Truncated {
		t.Fatal("expected Unicode content to be truncated")
	}
}

func ids(memories []store.Memory) string {
	out := make([]string, 0, len(memories))
	for _, mem := range memories {
		out = append(out, mem.ID)
	}
	return strings.Join(out, ",")
}
