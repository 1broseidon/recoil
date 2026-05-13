package cmd

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type searchOptions struct {
	scope          scopeOptions
	filters        memoryFilterOptions
	limit          int
	minimal        bool
	maxChars       int
	hybrid         bool
	hybridProvider string
	hybridModel    string
	hybridPool     int
	fusionK        int
}

func newSearchCommand() *cobra.Command {
	var searchOpts searchOptions
	c := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memories with SQLite FTS",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("query is empty")
			}
			sc, err := resolveReadScope(cmd, searchOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			params, err := searchParams(query, sc, searchOpts.filters, searchOpts.limit)
			if err != nil {
				return err
			}
			explicitLifecycle := params.Lifecycle != store.LifecycleAny || params.Validity != ""
			if !explicitLifecycle {
				params.Lifecycle = store.LifecycleCurrent
			}
			var current []store.Memory
			if searchOpts.hybrid {
				current, err = runHybridSearch(context.Background(), st, params, searchOpts)
			} else {
				current, err = st.Search(context.Background(), params)
			}
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "search_result", current)
			}
			if searchOpts.minimal {
				for _, r := range current {
					writeMinimalMemory(w, r, true)
				}
				return nil
			}
			var historical []store.Memory
			if !explicitLifecycle {
				historicalParams := params
				historicalParams.Lifecycle = store.LifecycleHistorical
				historical, err = st.Search(context.Background(), historicalParams)
				if err != nil {
					return err
				}
			}

			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(current))},
				{k: "history_count", v: fmt.Sprintf("%d", len(historical))},
			}, searchMemoryBlocks(current, historical, searchOpts.maxChars))
		},
	}
	addScopeFlags(c, &searchOpts.scope)
	addMemoryFilterFlags(c, &searchOpts.filters)
	c.Flags().IntVar(&searchOpts.limit, "limit", 5, "maximum number of memories to return")
	c.Flags().BoolVar(&searchOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().IntVar(&searchOpts.maxChars, "max-chars", 4000, "maximum characters of memory content to print")
	c.Flags().BoolVar(&searchOpts.hybrid, "hybrid", false, "fuse FTS5 and embedding similarity via RRF (requires indexed embeddings)")
	c.Flags().StringVar(&searchOpts.hybridProvider, "hybrid-provider", embedding.OpenRouterProvider, "embedding provider for --hybrid")
	c.Flags().StringVar(&searchOpts.hybridModel, "hybrid-model", embedding.DefaultOpenRouterModel, "embedding model for --hybrid")
	c.Flags().IntVar(&searchOpts.hybridPool, "hybrid-pool", 50, "candidate pool size per retrieval method before fusion")
	c.Flags().IntVar(&searchOpts.fusionK, "fusion-k", 60, "RRF fusion constant (standard: 60)")
	return c
}

// runHybridSearch fuses FTS5 and embedding-similarity rankings via Reciprocal
// Rank Fusion. The flow:
//
//  1. Pull a wider FTS5 candidate pool than the operator's --limit.
//  2. Embed the query with the configured provider, then SemanticSearch over
//     the same pool of indexed embeddings.
//  3. RRF-fuse the two rankings (score = sum of 1/(k + rank_method)) and trim
//     to --limit.
//
// We honor every filter the FTS path honors — scope, role, claim_key,
// validity, source kind, etc. — by reusing the same store.SearchParams for
// both legs, only swapping Query for the embedding vector on the semantic
// side. Result: identical filter semantics across both rankings.
func runHybridSearch(ctx context.Context, st *store.Store, p store.SearchParams, opts searchOptions) ([]store.Memory, error) {
	pool := opts.hybridPool
	if pool < opts.limit {
		pool = opts.limit
	}
	ftsParams := p
	ftsParams.Limit = pool
	ftsResults, err := st.Search(ctx, ftsParams)
	if err != nil {
		return nil, fmt.Errorf("fts leg: %w", err)
	}

	provider, err := newEmbeddingProvider(opts.hybridProvider, opts.hybridModel)
	if err != nil {
		return nil, fmt.Errorf("embedding provider: %w", err)
	}
	queryVec, err := provider.Embed(ctx, p.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	semParams := store.SemanticSearchParams{
		QueryVector: queryVec,
		Provider:    provider.Name(),
		Model:       provider.Model(),
		ScopeKind:   p.ScopeKind,
		ScopeID:     p.ScopeID,
		SourceKind:  p.SourceKind,
		SourceAgent: p.SourceAgent,
		SourcePath:  p.SourcePath,
		Role:        p.Role,
		ClaimKey:    p.ClaimKey,
		Validity:    p.Validity,
		Since:       p.Since,
		Before:      p.Before,
		Limit:       pool,
		Lifecycle:   p.Lifecycle,
	}
	semResults, err := st.SemanticSearch(ctx, semParams)
	if err != nil {
		return nil, fmt.Errorf("semantic leg: %w", err)
	}

	type scored struct {
		mem   store.Memory
		score float64
	}
	byID := map[string]*scored{}
	k := float64(opts.fusionK)
	for i, m := range ftsResults {
		if _, ok := byID[m.ID]; !ok {
			byID[m.ID] = &scored{mem: m}
		}
		byID[m.ID].score += 1.0 / (k + float64(i+1))
	}
	for i, m := range semResults {
		if _, ok := byID[m.ID]; !ok {
			byID[m.ID] = &scored{mem: m}
		}
		byID[m.ID].score += 1.0 / (k + float64(i+1))
	}
	fused := make([]scored, 0, len(byID))
	for _, s := range byID {
		fused = append(fused, *s)
	}
	sort.Slice(fused, func(i, j int) bool {
		if math.Abs(fused[i].score-fused[j].score) < 1e-9 {
			return fused[i].mem.CreatedAt > fused[j].mem.CreatedAt
		}
		return fused[i].score > fused[j].score
	})
	limit := opts.limit
	if limit <= 0 {
		limit = 5
	}
	if len(fused) > limit {
		fused = fused[:limit]
	}
	out := make([]store.Memory, len(fused))
	for i, f := range fused {
		out[i] = f.mem
	}
	return out, nil
}

func searchMemoryBlocks(current, historical []store.Memory, maxChars int) string {
	var b strings.Builder
	if len(current) == 0 {
		b.WriteString("No current memories found.")
	} else {
		b.WriteString("## Current Results\n\n")
		b.WriteString(memoryBlocks(current, maxChars, true))
	}
	if len(historical) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## Historical Results\n\n")
		b.WriteString(memoryBlocks(historical, maxChars, true))
	}
	return strings.TrimRight(b.String(), "\n")
}
