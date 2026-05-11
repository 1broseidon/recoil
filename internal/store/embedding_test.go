package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/embedding"
)

func TestSemanticSearchUsesEmbeddingSidecar(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	nonGoal, _, err := st.AddMemory(ctx, AddMemoryParams{
		Role:      "decision",
		Content:   "Recoil v0 has no daemon, no hosted service, and no cloud sync.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "v0.non-goals",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := st.AddMemory(ctx, AddMemoryParams{
		Role:      "decision",
		Content:   "Recoil uses mattn/go-sqlite3 through CGO with SQLITE_ENABLE_FTS5.",
		ScopeKind: "project",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "dependency.sqlite-driver",
	})
	if err != nil {
		t.Fatal(err)
	}

	provider := embedding.NewLocalProvider("")
	for _, mem := range []Memory{*nonGoal, *other} {
		vector, err := provider.Embed(ctx, EmbeddingText(mem))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertEmbedding(ctx, mem, provider.Name(), provider.Model(), vector); err != nil {
			t.Fatal(err)
		}
	}
	queryVector, err := provider.Embed(ctx, "always-on background remote synchronization")
	if err != nil {
		t.Fatal(err)
	}
	results, err := st.SemanticSearch(ctx, SemanticSearchParams{
		QueryVector: queryVector,
		Provider:    provider.Name(),
		Model:       provider.Model(),
		ScopeKind:   "project",
		ScopeID:     "project-1",
		Limit:       2,
		Lifecycle:   LifecycleCurrent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected semantic results")
	}
	if results[0].ID != nonGoal.ID {
		t.Fatalf("expected non-goal memory first, got %s", results[0].ID)
	}
}
