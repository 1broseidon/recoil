package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/store"
)

// TestRemoteArtifactRanksAsGuidance pins the visibility of synced remote
// channel artifacts in mixed-corpus search. Before the fix, a remote_artifact
// decision with high query coverage was ranked last because (a) the +0.75
// guidance bonus in runSignalSearch only fired for source_kind=direct and
// (b) channel://… source paths tripped the path-depth penalty in the
// sourcequality prior even though they aren't filesystem paths.
func TestRemoteArtifactRanksAsGuidance(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rank.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	type seed struct {
		role, content, sourceKind, sourceAgent, sourcePath, claimKey string
	}
	seeds := []seed{
		{
			role:       "decision",
			content:    "Search ranking uses BM25 plus per-role and per-claim-key boosts inside the SQL ORDER BY. Source kind nudges apply to direct and session_evidence memories.",
			sourceKind: "direct",
			claimKey:   "retrieval.bm25",
		},
		{
			role:       "decision",
			content:    "FTS5 search builds an OR-joined query across terms, then re-ranks with a sourcequality prior that boosts operational docs like README, AGENTS, CLAUDE.",
			sourceKind: "direct",
			claimKey:   "retrieval.or-terms",
		},
		{
			role:       "decision",
			content:    "Source kind boosts in the SQL ORDER BY favor direct decisions and session evidence over mined file chunks for general search results.",
			sourceKind: "direct",
			claimKey:   "retrieval.source-kind-boost",
		},
		{
			role:       "decision",
			content:    "Wake composes a layered context window across decisions, recent memories, and entity profiles, but has no dedicated remote-artifact or swarm lane today.",
			sourceKind: "direct",
			claimKey:   "wake.layered",
		},
		{
			role:        "decision",
			content:     "Remote channel artifacts import correctly into the local DB and FTS index but get buried behind older local memories in mixed-corpus search because of OR-term matching plus source-kind boosts favoring direct memories.",
			sourceKind:  "remote_artifact",
			sourceAgent: "codex",
			sourcePath:  "channel://measure/codex/art_xyz",
			claimKey:    "retrieval.remote-artifact-visibility",
		},
	}
	for _, s := range seeds {
		_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:        s.role,
			Content:     s.content,
			SourceKind:  s.sourceKind,
			SourceAgent: s.sourceAgent,
			SourcePath:  s.sourcePath,
			ScopeKind:   "session",
			ScopeID:     "rank",
			Validity:    "active",
			ClaimKey:    s.claimKey,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	type expect struct {
		query  string
		maxPos int
	}
	cases := []expect{
		// Topical overlap with the remote artifact's content (coverage >= 0.5);
		// the guidance bonus should make it competitive with local decisions.
		{query: "remote artifact search retrieval ranking", maxPos: 3},
		// Near-verbatim match against the remote artifact's wording; it should
		// be the single best hit.
		{query: "channel artifact buried in search", maxPos: 1},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			results, err := runSignalSearch(ctx, st, store.SearchParams{
				Query:        tc.query,
				ScopeKind:    "session",
				ScopeID:      "rank",
				Limit:        10,
				SignalRerank: true,
				Lifecycle:    store.LifecycleCurrent,
			})
			if err != nil {
				t.Fatal(err)
			}
			pos := -1
			for i, r := range results {
				if r.SourceKind == "remote_artifact" {
					pos = i + 1
					break
				}
			}
			if pos < 1 || pos > tc.maxPos {
				for i, r := range results {
					snippet := strings.ReplaceAll(r.Content, "\n", " ")
					if len(snippet) > 70 {
						snippet = snippet[:70] + "..."
					}
					t.Logf("rank %d: score=%6.3f source_kind=%-16s claim_key=%-40s %s",
						i+1, r.Score, r.SourceKind, r.ClaimKey, snippet)
				}
				t.Fatalf("remote_artifact ranked %d for %q; want top %d", pos, tc.query, tc.maxPos)
			}
		})
	}
}
