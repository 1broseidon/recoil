package cmd

import (
	"context"
	"sort"
	"strings"

	"github.com/1broseidon/recoil/internal/retrieval"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

type relatedClaim struct {
	ID       string `json:"id"`
	ClaimKey string `json:"claim_key"`
	Role     string `json:"role"`
	Excerpt  string `json:"excerpt,omitempty"`
}

func findRelatedGuidance(ctx context.Context, st *store.Store, sc scope.Scope, content, excludeID string) ([]store.Memory, error) {
	query := strings.TrimSpace(content)
	if tokens := retrieval.SignificantTokens(content); len(tokens) > 0 {
		query = strings.Join(tokens, " ")
	}
	if query == "" {
		return nil, nil
	}
	memories, err := st.Search(ctx, store.SearchParams{
		Query:        query,
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		Lifecycle:    store.LifecycleCurrent,
		Limit:        10,
		SignalRerank: false,
	})
	if err != nil {
		return nil, err
	}
	type scored struct {
		mem      store.Memory
		coverage float64
	}
	seen := map[string]bool{}
	var scoredResults []scored
	for _, mem := range memories {
		if mem.ID == excludeID || strings.TrimSpace(mem.ClaimKey) == "" || !isGuidanceRole(mem.Role) || seen[mem.ID] {
			continue
		}
		coverage := signalTokenCoverage(content, mem)
		if coverage < 0.5 {
			continue
		}
		seen[mem.ID] = true
		scoredResults = append(scoredResults, scored{mem: mem, coverage: coverage})
	}
	sort.SliceStable(scoredResults, func(i, j int) bool {
		if scoredResults[i].coverage == scoredResults[j].coverage {
			return scoredResults[i].mem.CreatedAt > scoredResults[j].mem.CreatedAt
		}
		return scoredResults[i].coverage > scoredResults[j].coverage
	})
	if len(scoredResults) > 3 {
		scoredResults = scoredResults[:3]
	}
	out := make([]store.Memory, 0, len(scoredResults))
	for _, item := range scoredResults {
		out = append(out, item.mem)
	}
	return out, nil
}

func relatedClaimsFromMemories(memories []store.Memory) []relatedClaim {
	out := make([]relatedClaim, 0, len(memories))
	for _, mem := range memories {
		out = append(out, relatedClaim{
			ID:       mem.ID,
			ClaimKey: mem.ClaimKey,
			Role:     mem.Role,
			Excerpt:  truncateText(oneLine(mem.Content), 80),
		})
	}
	return out
}

func appendRelatedClaimFrontmatter(meta []kv, prefix string, claims []relatedClaim) []kv {
	for _, claim := range claims {
		meta = append(meta,
			kv{k: prefix + "_claim_key", v: claim.ClaimKey + " (" + claim.ID + ")"},
			kv{k: prefix + "_id", v: claim.ID},
		)
	}
	return meta
}
