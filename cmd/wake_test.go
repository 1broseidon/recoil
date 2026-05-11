package cmd

import (
	"strings"
	"testing"

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
	body := layeredMemoryBlocks(layers, 220, true)
	if len(body) > 220 {
		t.Fatalf("wake body exceeded hard budget: %d\n%s", len(body), body)
	}
	if !strings.Contains(body, "L0 Current Context") {
		t.Fatalf("expected section heading, got:\n%s", body)
	}
	if !strings.Contains(body, "source_path: HANDOFF.md") {
		t.Fatalf("expected source metadata, got:\n%s", body)
	}
}

func ids(memories []store.Memory) string {
	out := make([]string, 0, len(memories))
	for _, mem := range memories {
		out = append(out, mem.ID)
	}
	return strings.Join(out, ",")
}
