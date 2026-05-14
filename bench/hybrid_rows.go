package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/1broseidon/recoil/internal/store"
)

// hybridRerankCandidates re-ranks an already-retrieved set of recoil rows by
// fusing the existing FTS rank (positional) with cosine similarity over
// embeddings. Returns the rows reordered, with len <= topK.
//
// Used by the LoCoMo and BEAM QA harnesses to opt into hybrid retrieval
// without re-implementing the FTS leg — the rows arrive already FTS-ranked,
// and this function adds the embedding leg + RRF fusion.
//
// If embedding fails, the original FTS order is returned (graceful fallback).
func hybridRerankCandidates(ctx context.Context, hc *hybridConfig, question string, rows []store.Memory, topK int) ([]store.Memory, error) {
	if hc == nil || len(rows) == 0 {
		if topK > 0 && len(rows) > topK {
			return rows[:topK], nil
		}
		return rows, nil
	}
	inputs := make([]string, 0, len(rows)+1)
	inputs = append(inputs, truncForEmbedding(question, hc.maxEmbedChars))
	// chunkOwners tracks which row each input slot belongs to; query slot is -1.
	chunkOwners := []int{-1}
	for i, row := range rows {
		var chunks []string
		if hc.chunkChars > 0 {
			chunks = chunkText(row.Content, hc.chunkChars, hc.chunkOverlap)
		} else {
			chunks = []string{truncForEmbedding(row.Content, hc.maxEmbedChars)}
		}
		for _, chunk := range chunks {
			inputs = append(inputs, chunk)
			chunkOwners = append(chunkOwners, i)
		}
	}
	vectors, _, err := hc.embedBatch(ctx, inputs)
	if err != nil {
		// Graceful fallback: return original FTS order, capped at topK.
		out := rows
		if topK > 0 && len(out) > topK {
			out = out[:topK]
		}
		return out, fmt.Errorf("embed failed (kept FTS order): %w", err)
	}
	queryVec := vectors[0]
	bestPerRow := make(map[int]float64, len(rows))
	for j := 1; j < len(vectors); j++ {
		owner := chunkOwners[j]
		sim := cosine(queryVec, vectors[j])
		if cur, ok := bestPerRow[owner]; !ok || sim > cur {
			bestPerRow[owner] = sim
		}
	}
	type scored struct {
		idx   int
		score float64
	}
	embedScored := make([]scored, 0, len(bestPerRow))
	for i, s := range bestPerRow {
		embedScored = append(embedScored, scored{idx: i, score: s})
	}
	sort.Slice(embedScored, func(i, j int) bool { return embedScored[i].score > embedScored[j].score })
	embedRank := map[int]int{}
	for r, s := range embedScored {
		embedRank[s.idx] = r + 1
	}
	k := float64(hc.fusionK)
	if k <= 0 {
		k = 60
	}
	fusedScores := make([]scored, len(rows))
	for i := range rows {
		s := 0.0
		// FTS rank is just the original position +1.
		s += 1.0 / (k + float64(i+1))
		if r, ok := embedRank[i]; ok {
			s += 1.0 / (k + float64(r))
		}
		fusedScores[i] = scored{idx: i, score: s}
	}
	sort.Slice(fusedScores, func(i, j int) bool { return fusedScores[i].score > fusedScores[j].score })
	out := make([]store.Memory, 0, len(fusedScores))
	for _, fs := range fusedScores {
		out = append(out, rows[fs.idx])
		if topK > 0 && len(out) >= topK {
			break
		}
	}
	return out, nil
}

// buildHybridConfig parses the standard --hybrid-* flag set into a
// hybridConfig, reusing OpenRouter/Ollama default plumbing.
func buildHybridConfig(provider, model, ollamaHost string, maxEmbedChars, chunkChars, chunkOverlap, fusionK, ftsPool, embedPool int) (*hybridConfig, error) {
	hc := &hybridConfig{
		embedProvider: provider,
		ollamaHost:    ollamaHost,
		maxEmbedChars: maxEmbedChars,
		chunkChars:    chunkChars,
		chunkOverlap:  chunkOverlap,
		fusionK:       fusionK,
		ftsPoolSize:   ftsPool,
		embedPoolSize: embedPool,
	}
	switch provider {
	case "", "openrouter":
		client, err := NewOpenRouterClient()
		if err != nil {
			return nil, err
		}
		hc.client = client
		hc.embedProvider = "openrouter"
		if model == "" {
			model = "openai/text-embedding-3-small"
		}
	case "ollama":
		if model == "" {
			model = "nomic-embed-text"
		}
		if hc.maxEmbedChars == 0 {
			hc.maxEmbedChars = 6000
		}
	default:
		return nil, fmt.Errorf("unknown embed-provider %q", provider)
	}
	hc.embedModel = model
	return hc, nil
}
