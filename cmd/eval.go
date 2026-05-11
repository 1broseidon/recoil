package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/embedding"
	recoileval "github.com/1broseidon/recoil/internal/eval"
	"github.com/1broseidon/recoil/internal/mine"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

const defaultEvalFixture = "eval/fixtures.jsonl"

const (
	retrievalFTS      = "fts"
	retrievalSemantic = "semantic"
	retrievalHybrid   = "hybrid"
)

type evalOptions struct {
	keepDB    bool
	retrieval string
	provider  string
	model     string
}

type evalRunResult struct {
	FixturePath    string                  `json:"fixture_path"`
	DBPath         string                  `json:"db_path"`
	TemporaryDB    bool                    `json:"temporary_db"`
	Retrieval      string                  `json:"retrieval"`
	Provider       string                  `json:"provider,omitempty"`
	Model          string                  `json:"model,omitempty"`
	Indexed        int                     `json:"indexed_embeddings,omitempty"`
	SeededMemories int                     `json:"seeded_memories"`
	MinedChunks    int                     `json:"mined_chunks,omitempty"`
	Summary        recoileval.Summary      `json:"summary"`
	Cases          []recoileval.CaseResult `json:"cases"`
}

type evalSeed struct {
	fixtureToMemory map[string]store.Memory
	memoryToFixture map[string]string
}

func newEvalCommand() *cobra.Command {
	var evalOpts evalOptions
	c := &cobra.Command{
		Use:   "eval [fixture]",
		Short: "Run retrieval eval fixtures",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fixturePath := defaultEvalFixture
			if len(args) == 1 {
				fixturePath = args[0]
			}
			fixture, err := recoileval.Load(fixturePath)
			if err != nil {
				return err
			}
			retrieval, err := normalizeRetrievalMode(evalOpts.retrieval)
			if err != nil {
				return err
			}

			dir, err := os.MkdirTemp("", "recoil-eval-*")
			if err != nil {
				return err
			}
			if !evalOpts.keepDB {
				defer os.RemoveAll(dir)
			}
			dbPath := filepath.Join(dir, "recoil.db")
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			seed, err := seedEvalMemories(ctx, st, fixture)
			if err != nil {
				return err
			}
			mined, err := seedEvalCorpora(ctx, st, fixturePath, fixture)
			if err != nil {
				return err
			}

			var provider embedding.Provider
			indexed := 0
			if retrieval != retrievalFTS {
				provider, err = newEmbeddingProvider(evalOpts.provider, evalOpts.model)
				if err != nil {
					return err
				}
				indexed, err = indexEmbeddings(ctx, st, provider, store.ListParams{
					Limit:          1000,
					IncludeDeleted: true,
					Lifecycle:      store.LifecycleAny,
				})
				if err != nil {
					return err
				}
			}

			caseResults := make([]recoileval.CaseResult, 0, len(fixture.Cases))
			for _, tc := range fixture.Cases {
				start := time.Now()
				resultIDs, err := runEvalCase(ctx, st, seed, tc, retrieval, provider)
				elapsed := time.Since(start)
				if err != nil {
					return fmt.Errorf("%s: %w", tc.ID, err)
				}
				caseResults = append(caseResults, recoileval.ScoreCase(tc, resultIDs, elapsed))
			}

			result := evalRunResult{
				FixturePath:    fixturePath,
				DBPath:         dbPath,
				TemporaryDB:    !evalOpts.keepDB,
				Retrieval:      retrieval,
				Indexed:        indexed,
				SeededMemories: len(seed.fixtureToMemory),
				MinedChunks:    mined,
				Summary:        recoileval.Summarize(caseResults),
				Cases:          caseResults,
			}
			if provider != nil {
				result.Provider = provider.Name()
				result.Model = provider.Model()
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "eval_result", result)
			}
			return frontmatter(w, evalFrontmatter(result), evalResultLines(result.Cases))
		},
	}
	c.Flags().BoolVar(&evalOpts.keepDB, "keep-db", false, "keep the temporary eval database after the run")
	c.Flags().StringVar(&evalOpts.retrieval, "retrieval", retrievalFTS, "retrieval mode: fts, semantic, or hybrid")
	c.Flags().StringVar(&evalOpts.provider, "embed-provider", embedding.DefaultLocalProvider, "embedding provider for semantic or hybrid eval")
	c.Flags().StringVar(&evalOpts.model, "embed-model", embedding.DefaultLocalModel, "embedding model for semantic or hybrid eval")
	return c
}

func evalFrontmatter(result evalRunResult) []kv {
	meta := []kv{
		{k: "fixture", v: result.FixturePath},
		{k: "db_path", v: result.DBPath},
		{k: "temporary_db", v: fmt.Sprintf("%t", result.TemporaryDB)},
		{k: "retrieval", v: result.Retrieval},
	}
	if result.Provider != "" {
		meta = append(meta,
			kv{k: "provider", v: result.Provider},
			kv{k: "model", v: result.Model},
			kv{k: "indexed_embeddings", v: fmt.Sprintf("%d", result.Indexed)},
		)
	}
	meta = append(meta,
		kv{k: "seeded_memories", v: fmt.Sprintf("%d", result.SeededMemories)},
	)
	if result.MinedChunks > 0 {
		meta = append(meta, kv{k: "mined_chunks", v: fmt.Sprintf("%d", result.MinedChunks)})
	}
	meta = append(meta,
		kv{k: "case_count", v: fmt.Sprintf("%d", result.Summary.TotalCases)},
		kv{k: "passed", v: fmt.Sprintf("%d", result.Summary.PassedCases)},
		kv{k: "failed", v: fmt.Sprintf("%d", result.Summary.FailedCases)},
		kv{k: "recall_at_k", v: fmt.Sprintf("%.4f", result.Summary.RecallAtK)},
		kv{k: "mrr", v: fmt.Sprintf("%.4f", result.Summary.MRR)},
		kv{k: "empty_result_accuracy", v: fmt.Sprintf("%.4f", result.Summary.EmptyResultAccuracy)},
		kv{k: "scope_isolation_failures", v: fmt.Sprintf("%d", result.Summary.ScopeIsolationFailures)},
		kv{k: "stale_demotion_failures", v: fmt.Sprintf("%d", result.Summary.StaleDemotionFailures)},
		kv{k: "wake_safety_failures", v: fmt.Sprintf("%d", result.Summary.WakeSafetyFailures)},
		kv{k: "latency_ms", v: fmt.Sprintf("%d", result.Summary.LatencyMS)},
	)
	return meta
}

func seedEvalMemories(ctx context.Context, st *store.Store, fixture recoileval.Fixture) (evalSeed, error) {
	seed := evalSeed{
		fixtureToMemory: make(map[string]store.Memory, len(fixture.Memories)),
		memoryToFixture: make(map[string]string, len(fixture.Memories)),
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, rec := range fixture.Memories {
		metadata, err := evalMetadataJSON(rec)
		if err != nil {
			return evalSeed{}, err
		}
		order := rec.CreatedAtOrder
		if order == 0 {
			order = i + 1
		}
		params := store.AddMemoryParams{
			Role:         rec.Role,
			Content:      rec.Content,
			SourceAgent:  rec.SourceAgent,
			SourcePath:   rec.SourcePath,
			SourceRef:    rec.SourceRef,
			ScopeKind:    rec.Scope.Kind,
			ScopeID:      rec.Scope.ID,
			MetadataJSON: metadata,
			Validity:     rec.Metadata["validity"],
			ClaimKey:     rec.Metadata["claim_key"],
			Supersedes:   rec.Metadata["supersedes"],
			SupersededBy: rec.Metadata["superseded_by"],
			CreatedAt:    base.Add(time.Duration(order) * time.Second).Format(time.RFC3339),
		}
		switch rec.Scope.Kind {
		case "project":
			params.ProjectID = rec.Scope.ID
		case "session":
			params.SessionID = rec.Scope.ID
		}
		mem, _, err := st.AddMemory(ctx, params)
		if err != nil {
			return evalSeed{}, fmt.Errorf("seed %s: %w", rec.ID, err)
		}
		seed.fixtureToMemory[rec.ID] = *mem
		seed.memoryToFixture[mem.ID] = rec.ID
	}
	return seed, nil
}

func evalMetadataJSON(rec recoileval.MemoryRecord) (string, error) {
	metadata := make(map[string]string, len(rec.Metadata)+2)
	for k, v := range rec.Metadata {
		metadata[k] = v
	}
	metadata["fixture_id"] = rec.ID
	if rec.CreatedAtOrder != 0 {
		metadata["created_at_order"] = strconv.Itoa(rec.CreatedAtOrder)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func runEvalCase(ctx context.Context, st *store.Store, seed evalSeed, tc recoileval.CaseRecord, retrieval string, provider embedding.Provider) ([]string, error) {
	limit := evalLimit(tc)
	switch tc.Mode {
	case "search":
		results, err := runEvalSearch(ctx, st, provider, retrieval, store.SearchParams{
			Query:       tc.Query,
			ScopeKind:   tc.Scope.Kind,
			ScopeID:     tc.Scope.ID,
			Role:        tc.Role,
			ClaimKey:    tc.ClaimKey,
			Validity:    tc.Validity,
			SourceAgent: tc.SourceAgent,
			SourcePath:  tc.SourcePath,
			Limit:       limit,
			Lifecycle:   evalLifecycle(tc, store.LifecycleCurrent),
		})
		if err != nil {
			return nil, err
		}
		return evalResultKeys(seed, tc, results), nil
	case "list":
		results, err := st.List(ctx, store.ListParams{
			ScopeKind:   tc.Scope.Kind,
			ScopeID:     tc.Scope.ID,
			Role:        tc.Role,
			ClaimKey:    tc.ClaimKey,
			Validity:    tc.Validity,
			SourceAgent: tc.SourceAgent,
			SourcePath:  tc.SourcePath,
			Limit:       limit,
			Lifecycle:   evalLifecycle(tc, store.LifecycleAny),
		})
		if err != nil {
			return nil, err
		}
		return evalResultKeys(seed, tc, results), nil
	case "wake":
		fetchLimit := wakeFetchLimit(limit)
		var queryResults []store.Memory
		if strings.TrimSpace(tc.Query) != "" {
			results, err := runEvalSearch(ctx, st, provider, retrieval, store.SearchParams{
				Query:       tc.Query,
				ScopeKind:   tc.Scope.Kind,
				ScopeID:     tc.Scope.ID,
				Role:        tc.Role,
				ClaimKey:    tc.ClaimKey,
				Validity:    tc.Validity,
				SourceAgent: tc.SourceAgent,
				SourcePath:  tc.SourcePath,
				Limit:       fetchLimit,
				Lifecycle:   evalLifecycle(tc, store.LifecycleCurrent),
			})
			if err != nil {
				return nil, err
			}
			queryResults = results
		}
		recent, err := st.List(ctx, store.ListParams{
			ScopeKind:   tc.Scope.Kind,
			ScopeID:     tc.Scope.ID,
			Role:        tc.Role,
			ClaimKey:    tc.ClaimKey,
			Validity:    tc.Validity,
			SourceAgent: tc.SourceAgent,
			SourcePath:  tc.SourcePath,
			Limit:       fetchLimit,
			Lifecycle:   evalLifecycle(tc, store.LifecycleCurrent),
		})
		if err != nil {
			return nil, err
		}
		layers := buildWakeLayers(tc.Query, queryResults, recent, limit)
		return evalResultKeys(seed, tc, flattenWakeLayers(layers)), nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", tc.Mode)
	}
}

func normalizeRetrievalMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", retrievalFTS:
		return retrievalFTS, nil
	case retrievalSemantic:
		return retrievalSemantic, nil
	case retrievalHybrid:
		return retrievalHybrid, nil
	default:
		return "", fmt.Errorf("unsupported retrieval mode %q", value)
	}
}

func runEvalSearch(ctx context.Context, st *store.Store, provider embedding.Provider, retrieval string, params store.SearchParams) ([]store.Memory, error) {
	switch retrieval {
	case retrievalFTS:
		return st.Search(ctx, params)
	case retrievalSemantic:
		if provider == nil {
			return nil, fmt.Errorf("semantic retrieval requires an embedding provider")
		}
		vector, err := provider.Embed(ctx, params.Query)
		if err != nil {
			return nil, err
		}
		return st.SemanticSearch(ctx, semanticParamsFromSearch(params, vector, provider))
	case retrievalHybrid:
		if provider == nil {
			return nil, fmt.Errorf("hybrid retrieval requires an embedding provider")
		}
		return hybridSearch(ctx, st, provider, params)
	default:
		return nil, fmt.Errorf("unsupported retrieval mode %q", retrieval)
	}
}

func hybridSearch(ctx context.Context, st *store.Store, provider embedding.Provider, params store.SearchParams) ([]store.Memory, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 5
	}
	wideLimit := limit * 4
	if wideLimit < 20 {
		wideLimit = 20
	}
	ftsParams := params
	ftsParams.Limit = wideLimit
	ftsResults, err := st.Search(ctx, ftsParams)
	if err != nil {
		return nil, err
	}
	vector, err := provider.Embed(ctx, params.Query)
	if err != nil {
		return nil, err
	}
	semanticParams := semanticParamsFromSearch(params, vector, provider)
	semanticParams.Limit = wideLimit
	semanticResults, err := st.SemanticSearch(ctx, semanticParams)
	if err != nil {
		return nil, err
	}
	return mergeHybridResults(ftsResults, semanticResults, limit), nil
}

func mergeHybridResults(ftsResults, semanticResults []store.Memory, limit int) []store.Memory {
	type candidate struct {
		mem      store.Memory
		fts      float64
		semantic float64
		order    int
	}
	byID := make(map[string]*candidate, len(ftsResults)+len(semanticResults))
	order := 0
	add := func(mem store.Memory, ftsScore, semanticScore float64) {
		item, ok := byID[mem.ID]
		if !ok {
			item = &candidate{mem: mem, order: order}
			order++
			byID[mem.ID] = item
		}
		if ftsScore > item.fts {
			item.fts = ftsScore
		}
		if semanticScore > item.semantic {
			item.semantic = semanticScore
		}
	}
	for _, mem := range ftsResults {
		add(mem, mem.Score, 0)
	}
	for _, mem := range semanticResults {
		add(mem, 0, mem.Score)
	}
	candidates := make([]candidate, 0, len(byID))
	for _, item := range byID {
		item.mem.Score = 0.45*item.fts + 0.55*item.semantic
		candidates = append(candidates, *item)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].mem.Score == candidates[j].mem.Score {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].mem.Score > candidates[j].mem.Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	results := make([]store.Memory, 0, len(candidates))
	for _, item := range candidates {
		results = append(results, item.mem)
	}
	return results
}

func evalLifecycle(tc recoileval.CaseRecord, fallback string) string {
	if strings.TrimSpace(tc.Validity) != "" {
		return store.LifecycleAny
	}
	if strings.TrimSpace(tc.Lifecycle) != "" {
		return strings.TrimSpace(tc.Lifecycle)
	}
	return fallback
}

func evalLimit(tc recoileval.CaseRecord) int {
	if tc.Limit > 0 {
		return tc.Limit
	}
	if tc.Mode == "wake" {
		return 8
	}
	return 5
}

func evalResultKeys(seed evalSeed, tc recoileval.CaseRecord, memories []store.Memory) []string {
	if strings.TrimSpace(tc.MatchBy) == "source_path" {
		keys := make([]string, 0, len(memories))
		seen := make(map[string]bool, len(memories))
		for _, mem := range memories {
			path := strings.TrimSpace(mem.SourcePath)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			keys = append(keys, path)
		}
		return keys
	}
	return fixtureIDsFor(seed, memories)
}

func seedEvalCorpora(ctx context.Context, st *store.Store, fixturePath string, fixture recoileval.Fixture) (int, error) {
	if len(fixture.Corpora) == 0 {
		return 0, nil
	}
	fixtureDir := filepath.Dir(fixturePath)
	total := 0
	for _, corpus := range fixture.Corpora {
		path := corpus.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(fixtureDir, path)
		}
		role := corpus.Role
		if role == "" {
			role = "source"
		}
		agent := corpus.SourceAgent
		if agent == "" {
			agent = "recoil-eval"
		}
		collected, err := mine.Collect(mine.Options{
			Path:          path,
			SourceRoot:    path,
			IncludeHidden: corpus.IncludeHidden,
			MaxFileBytes:  corpus.MaxFileBytes,
			MaxChunkChars: corpus.MaxChunkChars,
		})
		if err != nil {
			return total, fmt.Errorf("corpus %s: %w", corpus.ID, err)
		}
		projectID := ""
		if corpus.Scope.Kind == "project" {
			projectID = corpus.Scope.ID
		}
		sessionID := ""
		if corpus.Scope.Kind == "session" {
			sessionID = corpus.Scope.ID
		}
		base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		for i, chunk := range collected.Chunks {
			metadata, err := mineMetadataJSON(chunk)
			if err != nil {
				return total, fmt.Errorf("corpus %s: %w", corpus.ID, err)
			}
			_, _, err = st.AddMemory(ctx, store.AddMemoryParams{
				Role:         role,
				Content:      chunk.Content,
				SourceAgent:  agent,
				SourcePath:   chunk.SourcePath,
				SourceRef:    chunk.SourceRef,
				ScopeKind:    corpus.Scope.Kind,
				ScopeID:      corpus.Scope.ID,
				ProjectID:    projectID,
				SessionID:    sessionID,
				MetadataJSON: metadata,
				CreatedAt:    base.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			})
			if err != nil {
				return total, fmt.Errorf("corpus %s chunk %d: %w", corpus.ID, i, err)
			}
			total++
		}
	}
	return total, nil
}

func fixtureIDsFor(seed evalSeed, memories []store.Memory) []string {
	ids := make([]string, 0, len(memories))
	for _, mem := range memories {
		id := mem.ID
		if fixtureID, ok := seed.memoryToFixture[mem.ID]; ok {
			id = fixtureID
		}
		ids = append(ids, id)
	}
	return ids
}

func evalResultLines(results []recoileval.CaseResult) string {
	var b strings.Builder
	for _, result := range results {
		status := "fail"
		if result.Passed {
			status = "pass"
		}
		fmt.Fprintf(&b, "## %s\n", result.ID)
		fmt.Fprintf(&b, "status: %s\n", status)
		fmt.Fprintf(&b, "mode: %s\n", result.Mode)
		if result.Category != "" {
			fmt.Fprintf(&b, "category: %s\n", result.Category)
		}
		fmt.Fprintf(&b, "latency_ms: %d\n", result.LatencyMS)
		fmt.Fprintf(&b, "recall: %.4f\n", result.Recall)
		fmt.Fprintf(&b, "reciprocal_rank: %.4f\n", result.ReciprocalRank)
		writeEvalIDs(&b, "results", result.ResultIDs)
		writeEvalIDs(&b, "expected_current", result.ExpectedCurrentIDs)
		writeEvalIDs(&b, "missing_current", result.MissingCurrentIDs)
		writeEvalIDs(&b, "forbidden_present", result.ForbiddenIDsPresent)
		writeEvalIDs(&b, "forbidden_current_present", result.ForbiddenCurrentIDsPresent)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeEvalIDs(b *strings.Builder, key string, ids []string) {
	if len(ids) == 0 {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, strings.Join(ids, ", "))
}
