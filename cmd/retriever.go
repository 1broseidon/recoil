package cmd

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/store"
)

type retrieverOptions struct {
	mode           string
	provider       embedding.Provider
	hybridProvider string
	hybridModel    string
	hybridPool     int
	fusionK        int
	limit          int
}

func runRetriever(ctx context.Context, st *store.Store, p store.SearchParams, opts retrieverOptions) ([]store.Memory, error) {
	switch opts.mode {
	case "", retrievalFTS:
		return runSignalSearch(ctx, st, p)
	case retrievalSemantic:
		provider := opts.provider
		if provider == nil {
			return nil, fmt.Errorf("semantic retrieval requires an embedding provider")
		}
		vector, err := provider.Embed(ctx, p.Query)
		if err != nil {
			return nil, err
		}
		return st.SemanticSearch(ctx, semanticParamsFromSearch(p, vector, provider))
	case retrievalHybrid:
		provider := opts.provider
		var err error
		if provider == nil {
			provider, err = newEmbeddingProvider(opts.hybridProvider, opts.hybridModel)
			if err != nil {
				return nil, fmt.Errorf("embedding provider: %w", err)
			}
		}
		return runHybridRetriever(ctx, st, p, provider, opts.hybridPool, opts.fusionK, opts.limit)
	default:
		return nil, fmt.Errorf("unsupported retrieval mode %q", opts.mode)
	}
}

func runHybridRetriever(ctx context.Context, st *store.Store, p store.SearchParams, provider embedding.Provider, pool, fusionK, limit int) ([]store.Memory, error) {
	if limit <= 0 {
		limit = p.Limit
	}
	if limit <= 0 {
		limit = 5
	}
	if pool < limit {
		pool = limit
	}
	if pool <= 0 {
		pool = limit * 4
	}
	if pool < 20 {
		pool = 20
	}
	ftsParams := p
	ftsParams.Limit = pool
	ftsResults, err := runSignalSearch(ctx, st, ftsParams)
	if err != nil {
		return nil, fmt.Errorf("fts leg: %w", err)
	}

	queryVec, err := provider.Embed(ctx, p.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	semParams := semanticParamsFromSearch(p, queryVec, provider)
	semParams.Limit = pool
	semResults, err := st.SemanticSearch(ctx, semParams)
	if err != nil {
		return nil, fmt.Errorf("semantic leg: %w", err)
	}

	if fusionK <= 0 {
		fusionK = 60
	}
	byID := map[string]*searchScoredMemory{}
	k := float64(fusionK)
	for i, mem := range ftsResults {
		if _, ok := byID[mem.ID]; !ok {
			byID[mem.ID] = &searchScoredMemory{mem: mem}
		}
		byID[mem.ID].score += 1.0 / (k + float64(i+1))
	}
	for i, mem := range semResults {
		if _, ok := byID[mem.ID]; !ok {
			byID[mem.ID] = &searchScoredMemory{mem: mem}
		}
		byID[mem.ID].score += 1.0 / (k + float64(i+1))
	}
	fused := make([]searchScoredMemory, 0, len(byID))
	for _, item := range byID {
		item.mem.Score = item.score
		fused = append(fused, *item)
	}
	sort.SliceStable(fused, func(i, j int) bool {
		if math.Abs(fused[i].score-fused[j].score) < 1e-9 {
			return fused[i].mem.CreatedAt > fused[j].mem.CreatedAt
		}
		return fused[i].score > fused[j].score
	})
	fused = filterStrictEntityResults(p.Query, fused)
	out := diversifySignalResults(fused, limit, p.Query)
	return expandDerivedSourceEvidence(ctx, st, p, out, limit)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
