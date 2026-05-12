package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/1broseidon/recoil/internal/store"
)

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
	want := "mem_handoff,mem_decision,mem_recent"
	if got != want {
		t.Fatalf("unexpected wake order: got %s, want %s", got, want)
	}
	if layers[0].Key != "l0_current_context" || len(layers[0].Memories) != 1 {
		t.Fatalf("expected handoff in L0, got %+v", layers[0])
	}
	if layers[1].Key != "l1_decisions_constraints" || len(layers[1].Memories) != 1 {
		t.Fatalf("expected decision in L1, got %+v", layers[1])
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
	if len(layers[0].Memories) != 1 || layers[0].Memories[0].ID != "mem_match" {
		t.Fatalf("expected query match in L0, got %+v", layers[0])
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
	if got := ids(layers[0].Memories); got != "mem_handoff_evidence" {
		t.Fatalf("expected one session evidence item in L0, got %s", got)
	}
	if strings.Contains(ids(layers[1].Memories), "mem_must_not_l1") {
		t.Fatalf("did not expect session evidence promoted into L1, got %+v", layers[1].Memories)
	}
	l2IDs := ids(layers[2].Memories)
	if strings.Count(l2IDs, "mem_evidence") > 2 || strings.Contains(l2IDs, "mem_evidence_3") {
		t.Fatalf("expected L2 session evidence cap of two, got %s", l2IDs)
	}
	if !strings.Contains(l2IDs, "mem_doc") {
		t.Fatalf("expected ordinary file evidence to remain eligible, got %s", l2IDs)
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
