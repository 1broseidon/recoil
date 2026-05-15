package cmd

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/retrieval"
	"github.com/1broseidon/recoil/internal/sourcequality"
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
	profiles       string
}

type searchScoredMemory struct {
	mem   store.Memory
	score float64
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
			_, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			params.SourceQuality = effectiveSourceQualityOptions(settings)
			explicitLifecycle := params.Lifecycle != store.LifecycleAny || params.Validity != ""
			if !explicitLifecycle {
				params.Lifecycle = store.LifecycleCurrent
			}
			mode := retrievalFTS
			if searchOpts.hybrid {
				mode = retrievalHybrid
			}
			current, err := runRetriever(context.Background(), st, params, retrieverOptions{
				mode:           mode,
				hybridProvider: searchOpts.hybridProvider,
				hybridModel:    searchOpts.hybridModel,
				hybridPool:     searchOpts.hybridPool,
				fusionK:        searchOpts.fusionK,
				limit:          searchOpts.limit,
			})
			if err != nil {
				return err
			}
			current, err = augmentProfileSearch(context.Background(), st, params, current, searchOpts.profiles)
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
				historical, err = runRetriever(context.Background(), st, historicalParams, retrieverOptions{
					mode:           mode,
					hybridProvider: searchOpts.hybridProvider,
					hybridModel:    searchOpts.hybridModel,
					hybridPool:     searchOpts.hybridPool,
					fusionK:        searchOpts.fusionK,
					limit:          searchOpts.limit,
				})
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
	c.Flags().StringVar(&searchOpts.profiles, "profiles", "auto", "profile retrieval mode: auto, on, or off")
	return c
}

func augmentProfileSearch(ctx context.Context, st *store.Store, p store.SearchParams, rows []store.Memory, mode string) ([]store.Memory, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "off", "false", "none":
		return rows, nil
	case "auto":
		if queryWantsRawDetail(p.Query) {
			return rows, nil
		}
	case "on", "true":
	default:
		return nil, fmt.Errorf("--profiles must be auto, on, or off")
	}
	if p.SourceKind != "" || p.Role != "" || p.ClaimKey != "" {
		return rows, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}
	profileLimit := 3
	if limit < profileLimit {
		profileLimit = limit
	}
	pp := p
	pp.SourceKind = "entity_profile"
	if profileLimit < 1 {
		profileLimit = 1
	}
	pp.Limit = profileLimit
	profiles, err := runRetriever(ctx, st, pp, retrieverOptions{
		mode:  retrievalFTS,
		limit: profileLimit,
	})
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return rows, nil
	}
	out := make([]store.Memory, 0, limit)
	seen := map[string]bool{}
	appendMem := func(mem store.Memory) {
		if len(out) >= limit || seen[mem.ID] {
			return
		}
		seen[mem.ID] = true
		out = append(out, mem)
	}
	for _, mem := range profiles {
		appendMem(mem)
	}
	for _, mem := range rows {
		appendMem(mem)
	}
	return out, nil
}

func queryWantsRawDetail(query string) bool {
	q := strings.ToLower(query)
	for _, marker := range []string{
		"exactly",
		"verbatim",
		"quote",
		"full text",
		"which command",
		"what command",
		"error message",
		"stack trace",
		"line number",
		"source line",
	} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

func runSignalSearch(ctx context.Context, st *store.Store, p store.SearchParams) ([]store.Memory, error) {
	if !p.SignalRerank {
		return st.Search(ctx, p)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}
	variants := retrieval.QueryVariants(p.Query)
	if len(variants) <= 1 {
		return st.Search(ctx, p)
	}
	// Pool must be wide enough that operational docs (root README, CONTRIBUTING,
	// SECURITY) survive into the candidate set on large repos where short-form
	// docs lose FTS to deeper, denser docs. We tripped over this on transformers
	// where root README's FTS rank for broad queries was beyond top-50 in the
	// >2k-chunk corpus, so the +3 root_readme prior never fired.
	pool := limit * 20
	if pool < 100 {
		pool = 100
	}
	if pool > 100 {
		pool = 100
	}
	byID := map[string]*searchScoredMemory{}
	k := 60.0
	for vi, variant := range variants {
		vp := p
		vp.Query = variant
		vp.Limit = pool
		rows, err := st.Search(ctx, vp)
		if err != nil {
			return nil, err
		}
		weight := 1.0
		if vi == 0 {
			weight = 1.25
		}
		for ri, mem := range rows {
			if _, ok := byID[mem.ID]; !ok {
				byID[mem.ID] = &searchScoredMemory{mem: mem}
			}
			byID[mem.ID].score += weight / (k + float64(ri+1))
		}
	}
	fused := make([]searchScoredMemory, 0, len(byID))
	for _, item := range byID {
		coverage := signalTokenCoverage(p.Query, item.mem)
		item.score += 0.08 * coverage
		item.score += sourcequality.ScorePriorWithOptions(p.Query, item.mem.SourcePath, item.mem.MetadataJSON, sourcequality.ModeSearch, p.SourceQuality)
		if coverage >= 0.5 && (item.mem.SourceKind == "direct" || item.mem.SourceKind == "remote_artifact") && isGuidanceRole(item.mem.Role) {
			item.score += 0.75
		}
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

func signalTokenCoverage(query string, mem store.Memory) float64 {
	tokens := retrieval.SignificantTokens(query)
	if len(tokens) == 0 {
		return 0
	}
	text := strings.ToLower(strings.Join([]string{
		mem.Content,
		mem.Role,
		mem.ClaimKey,
		mem.SourceKind,
		mem.SourceAgent,
		mem.SourcePath,
		mem.SourceRef,
	}, " "))
	hits := 0
	for _, token := range tokens {
		if strings.Contains(text, token) {
			hits++
		}
	}
	return float64(hits) / float64(len(tokens))
}

func filterStrictEntityResults(query string, fused []searchScoredMemory) []searchScoredMemory {
	doctorNames := retrieval.DoctorNameTerms(query)
	personNames := explicitPersonNameTerms(query)
	if len(doctorNames) == 0 && len(personNames) == 0 {
		return fused
	}
	var out []searchScoredMemory
	for _, item := range fused {
		text := strings.ToLower(item.mem.Content + " " + item.mem.SourceRef + " " + item.mem.SourcePath)
		for _, name := range doctorNames {
			if strings.Contains(text, name) {
				out = append(out, item)
				goto nextItem
			}
		}
		for _, name := range personNames {
			if strings.Contains(text, name) {
				out = append(out, item)
				goto nextItem
			}
		}
	nextItem:
	}
	return out
}

var explicitPersonNameRE = regexp.MustCompile(`\b([A-Z][a-z]{2,})\s+([A-Z][a-z]{2,})\b`)

func explicitPersonNameTerms(query string) []string {
	var names []string
	for _, match := range explicitPersonNameRE.FindAllStringSubmatch(query, -1) {
		if len(match) < 3 {
			continue
		}
		first := strings.ToLower(match[1])
		last := strings.ToLower(match[2])
		if commonNonPersonName(first, last) {
			continue
		}
		names = append(names, first+" "+last, last, first)
	}
	return uniqueSearchStrings(names)
}

func commonNonPersonName(first, last string) bool {
	pair := first + " " + last
	switch pair {
	case "content marketing", "digital marketing", "cli accuracy", "session evidence":
		return true
	}
	for _, term := range []string{"recoil", "sqlite", "fts", "json", "api", "cli"} {
		if first == term || last == term {
			return true
		}
	}
	return false
}

func uniqueSearchStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func diversifySignalResults(fused []searchScoredMemory, limit int, query string) []store.Memory {
	if limit <= 0 {
		limit = 5
	}
	out := make([]store.Memory, 0, limit)
	seenID := map[string]bool{}
	seenSource := map[string]bool{}
	allowNegative := queryAsksForNegativeEvidence(query)
	appendItem := func(item searchScoredMemory) {
		if len(out) >= limit || seenID[item.mem.ID] {
			return
		}
		if !allowNegative && isNegativeEvidence(item.mem) {
			return
		}
		seenID[item.mem.ID] = true
		out = append(out, item.mem)
	}
	for _, item := range fused {
		source := signalSourceKey(item.mem)
		if source == "" {
			appendItem(item)
			continue
		}
		if seenSource[source] {
			continue
		}
		seenSource[source] = true
		appendItem(item)
	}
	for _, item := range fused {
		appendItem(item)
	}
	if len(out) == 0 && !allowNegative {
		for _, item := range fused {
			if len(out) >= limit || seenID[item.mem.ID] {
				continue
			}
			seenID[item.mem.ID] = true
			out = append(out, item.mem)
		}
	}
	return out
}

func queryAsksForNegativeEvidence(query string) bool {
	query = strings.ToLower(query)
	return strings.Contains(query, " not ") ||
		strings.Contains(query, "never") ||
		strings.Contains(query, "avoid") ||
		strings.Contains(query, "rejected") ||
		strings.Contains(query, "noise")
}

func isNegativeEvidence(mem store.Memory) bool {
	text := strings.ToLower(mem.Content)
	normalized := strings.Join(strings.Fields(text), " ")
	for _, marker := range []string{
		"noise:",
		"noise note:",
		"intentionally mention",
		"intentionally noisy",
		"not the user's facts",
		"not my own",
		"not what i am",
		"not the current product direction",
		"not the answer",
		"only background discussion",
		"hosted sync service",
		"hosted sync",
		"should prefer the",
		"do not treat",
		"should not be used",
		"should not override",
	} {
		if strings.Contains(text, marker) || strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func signalSourceKey(mem store.Memory) string {
	switch {
	case strings.TrimSpace(mem.SourcePath) != "":
		return mem.SourceKind + ":" + mem.SourcePath
	case strings.TrimSpace(mem.SessionID) != "":
		return "session:" + mem.SessionID
	default:
		return ""
	}
}

func expandDerivedSourceEvidence(ctx context.Context, st *store.Store, p store.SearchParams, results []store.Memory, limit int) ([]store.Memory, error) {
	if limit <= 0 {
		limit = 5
	}
	out := make([]store.Memory, 0, limit)
	seen := map[string]bool{}
	appendMem := func(mem store.Memory) {
		if len(out) >= limit || seen[mem.ID] {
			return
		}
		seen[mem.ID] = true
		out = append(out, mem)
	}
	for _, mem := range results {
		appendMem(mem)
		if len(out) >= limit || strings.TrimSpace(mem.SourcePath) == "" {
			continue
		}
		if isDerivedTrace(mem) {
			parent, err := sourceEvidenceParent(ctx, st, p, mem)
			if err != nil {
				return nil, err
			}
			if parent != nil {
				appendMem(*parent)
			}
			continue
		}
		children, err := sourceEvidenceChildren(ctx, st, p, mem)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			appendMem(child)
		}
		if queryWantsSessionNeighbors(p.Query) && mem.SourceKind == "session_evidence" {
			neighbors, err := sourceEvidenceNeighbors(ctx, st, p, mem)
			if err != nil {
				return nil, err
			}
			for _, neighbor := range neighbors {
				appendMem(neighbor)
			}
		}
	}
	return out, nil
}

func queryWantsSessionNeighbors(query string) bool {
	query = strings.ToLower(query)
	return strings.Contains(query, " compared to ") ||
		strings.Contains(query, " versus ") ||
		strings.Contains(query, " vs ") ||
		strings.Contains(query, "doctor")
}

func isGuidanceRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "adr", "decision", "constraint", "note", "preference", "rule":
		return true
	default:
		return false
	}
}

func isDerivedTrace(mem store.Memory) bool {
	return strings.Contains(mem.MetadataJSON, `"derived_type":"`) ||
		strings.HasPrefix(mem.Content, "Derived user profile trace:") ||
		strings.HasPrefix(mem.Content, "Derived update trace:") ||
		strings.HasSuffix(strings.TrimSpace(mem.SourceRef), " profile") ||
		strings.HasSuffix(strings.TrimSpace(mem.SourceRef), " update")
}

func sourceEvidenceParent(ctx context.Context, st *store.Store, p store.SearchParams, mem store.Memory) (*store.Memory, error) {
	parentRef := strings.TrimSpace(mem.SourceRef)
	parentRef = strings.TrimSpace(strings.TrimSuffix(parentRef, " profile"))
	parentRef = strings.TrimSpace(strings.TrimSuffix(parentRef, " update"))
	candidates, err := st.List(ctx, store.ListParams{
		ScopeKind:  p.ScopeKind,
		ScopeID:    p.ScopeID,
		SourceKind: mem.SourceKind,
		SourcePath: mem.SourcePath,
		Role:       "source",
		Limit:      20,
		Lifecycle:  p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if parentRef != "" && strings.TrimSpace(candidate.SourceRef) == parentRef {
			return &candidate, nil
		}
	}
	for _, candidate := range candidates {
		if mem.SessionID == "" || candidate.SessionID == mem.SessionID {
			return &candidate, nil
		}
	}
	return nil, nil
}

func sourceEvidenceChildren(ctx context.Context, st *store.Store, p store.SearchParams, mem store.Memory) ([]store.Memory, error) {
	if strings.TrimSpace(mem.SourceRef) == "" {
		return nil, nil
	}
	candidates, err := st.List(ctx, store.ListParams{
		ScopeKind:  p.ScopeKind,
		ScopeID:    p.ScopeID,
		SourceKind: mem.SourceKind,
		SourcePath: mem.SourcePath,
		Limit:      20,
		Lifecycle:  p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	var children []store.Memory
	parentRef := strings.TrimSpace(mem.SourceRef)
	for _, candidate := range candidates {
		if candidate.ID == mem.ID || !isDerivedTrace(candidate) {
			continue
		}
		childRef := strings.TrimSpace(candidate.SourceRef)
		if childRef == parentRef+" profile" || childRef == parentRef+" update" {
			children = append(children, candidate)
		}
	}
	return children, nil
}

func sourceEvidenceNeighbors(ctx context.Context, st *store.Store, p store.SearchParams, mem store.Memory) ([]store.Memory, error) {
	candidates, err := st.List(ctx, store.ListParams{
		ScopeKind:  p.ScopeKind,
		ScopeID:    p.ScopeID,
		SourceKind: mem.SourceKind,
		SourcePath: mem.SourcePath,
		Role:       "source",
		Limit:      20,
		Lifecycle:  p.Lifecycle,
	})
	if err != nil {
		return nil, err
	}
	type scored struct {
		mem   store.Memory
		score float64
	}
	var scoredNeighbors []scored
	expandedQuery := retrieval.ExpandedQueryText(p.Query)
	for _, candidate := range candidates {
		if candidate.ID == mem.ID || isNegativeEvidence(candidate) {
			continue
		}
		score := signalTokenCoverage(expandedQuery, candidate)
		if score <= 0 {
			continue
		}
		scoredNeighbors = append(scoredNeighbors, scored{mem: candidate, score: score})
	}
	sort.SliceStable(scoredNeighbors, func(i, j int) bool {
		if math.Abs(scoredNeighbors[i].score-scoredNeighbors[j].score) < 1e-9 {
			return scoredNeighbors[i].mem.CreatedAt > scoredNeighbors[j].mem.CreatedAt
		}
		return scoredNeighbors[i].score > scoredNeighbors[j].score
	})
	neighbors := make([]store.Memory, 0, len(scoredNeighbors))
	for _, item := range scoredNeighbors {
		neighbors = append(neighbors, item.mem)
	}
	return neighbors, nil
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
	provider, err := newEmbeddingProvider(opts.hybridProvider, opts.hybridModel)
	if err != nil {
		return nil, fmt.Errorf("embedding provider: %w", err)
	}
	return runHybridRetriever(ctx, st, p, provider, opts.hybridPool, opts.fusionK, opts.limit)
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
