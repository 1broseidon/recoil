package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/embedding"
	recoileval "github.com/1broseidon/recoil/internal/eval"
	"github.com/1broseidon/recoil/internal/mine"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/sessionevidence"
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
	suite     string
	outPath   string
}

type evalSuiteResult struct {
	Suite     string                  `json:"suite"`
	Retrieval string                  `json:"retrieval"`
	Summary   recoileval.Summary      `json:"summary"`
	Fixtures  []evalRunResult         `json:"fixtures"`
	Cases     []recoileval.CaseResult `json:"cases"`
}

type evalRunResult struct {
	FixturePath          string                  `json:"fixture_path"`
	DBPath               string                  `json:"db_path"`
	TemporaryDB          bool                    `json:"temporary_db"`
	Retrieval            string                  `json:"retrieval"`
	Provider             string                  `json:"provider,omitempty"`
	Model                string                  `json:"model,omitempty"`
	Indexed              int                     `json:"indexed_embeddings,omitempty"`
	SeededMemories       int                     `json:"seeded_memories"`
	MinedChunks          int                     `json:"mined_chunks,omitempty"`
	SeededTranscripts    int                     `json:"seeded_transcripts,omitempty"`
	MinedSessionEvidence int                     `json:"mined_session_evidence,omitempty"`
	Summary              recoileval.Summary      `json:"summary"`
	Cases                []recoileval.CaseResult `json:"cases"`
}

type evalSeed struct {
	fixtureToMemory map[string]store.Memory
	memoryToFixture map[string]string
}

type evalCaseOutput struct {
	IDs   []string
	Texts []string
}

func newEvalCommand() *cobra.Command {
	var evalOpts evalOptions
	c := &cobra.Command{
		Use:   "eval [fixture]",
		Short: "Run retrieval eval fixtures",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			suite := strings.TrimSpace(evalOpts.suite)
			fixturePaths := []string{defaultEvalFixture}
			if len(args) == 1 {
				if suite != "" {
					return fmt.Errorf("--suite cannot be combined with an explicit fixture path")
				}
				fixturePaths = []string{args[0]}
			} else if suite != "" {
				paths, err := evalSuiteFixturePaths(suite)
				if err != nil {
					return err
				}
				fixturePaths = paths
			}
			retrieval, err := normalizeRetrievalMode(evalOpts.retrieval)
			if err != nil {
				return err
			}
			results := make([]evalRunResult, 0, len(fixturePaths))
			for _, fixturePath := range fixturePaths {
				result, err := runEvalFixture(fixturePath, evalOpts, retrieval)
				if err != nil {
					return err
				}
				results = append(results, result)
			}
			w := cmd.OutOrStdout()
			if len(results) == 1 {
				if err := writeEvalArtifacts(evalOpts.outPath, "eval_result", evalRunArtifactName(results[0]), results[0], evalFrontmatter(results[0]), evalResultLines(results[0].Cases)); err != nil {
					return err
				}
				if opts.json {
					if err := writeJSON(w, "eval_result", results[0]); err != nil {
						return err
					}
					return evalFailureError(results[0].Summary)
				}
				if err := frontmatter(w, evalFrontmatter(results[0]), evalResultLines(results[0].Cases)); err != nil {
					return err
				}
				return evalFailureError(results[0].Summary)
			}
			result := evalSuiteSummary(suite, retrieval, results)
			if err := writeEvalArtifacts(evalOpts.outPath, "eval_suite_result", evalSuiteArtifactName(result), result, evalSuiteFrontmatter(result), evalResultLines(result.Cases)); err != nil {
				return err
			}
			if opts.json {
				if err := writeJSON(w, "eval_suite_result", result); err != nil {
					return err
				}
				return evalFailureError(result.Summary)
			}
			if err := frontmatter(w, evalSuiteFrontmatter(result), evalResultLines(result.Cases)); err != nil {
				return err
			}
			return evalFailureError(result.Summary)
		},
	}
	c.Flags().BoolVar(&evalOpts.keepDB, "keep-db", false, "keep the temporary eval database after the run")
	c.Flags().StringVar(&evalOpts.suite, "suite", "", "named eval suite: default, embeddings, workflows, session-evidence, decisions, cli-hard")
	c.Flags().StringVar(&evalOpts.retrieval, "retrieval", retrievalFTS, "retrieval mode: fts, semantic, or hybrid")
	c.Flags().StringVar(&evalOpts.provider, "embed-provider", embedding.DefaultLocalProvider, "embedding provider for semantic or hybrid eval")
	c.Flags().StringVar(&evalOpts.model, "embed-model", embedding.DefaultLocalModel, "embedding model for semantic or hybrid eval")
	c.Flags().StringVar(&evalOpts.outPath, "out", "", "write normalized JSON and Markdown result artifacts to a directory, .json file, or .md file")
	return c
}

func runEvalFixture(fixturePath string, evalOpts evalOptions, retrieval string) (evalRunResult, error) {
	fixture, err := recoileval.Load(fixturePath)
	if err != nil {
		return evalRunResult{}, err
	}
	dir, err := os.MkdirTemp("", "recoil-eval-*")
	if err != nil {
		return evalRunResult{}, err
	}
	if !evalOpts.keepDB {
		defer os.RemoveAll(dir)
	}
	restoreEnv := isolateEvalStateDir(dir)
	defer restoreEnv()
	dbPath := filepath.Join(dir, "recoil.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return evalRunResult{}, err
	}
	defer st.Close()

	ctx := context.Background()
	seed, err := seedEvalMemories(ctx, st, fixture)
	if err != nil {
		return evalRunResult{}, err
	}
	mined, err := seedEvalCorpora(ctx, st, fixturePath, fixture)
	if err != nil {
		return evalRunResult{}, err
	}
	seededTranscripts, minedSessionEvidence, err := seedEvalTranscripts(ctx, st, fixturePath, fixture)
	if err != nil {
		return evalRunResult{}, err
	}

	var provider embedding.Provider
	indexed := 0
	if retrieval != retrievalFTS {
		provider, err = newEmbeddingProvider(evalOpts.provider, evalOpts.model)
		if err != nil {
			return evalRunResult{}, err
		}
		indexed, err = indexEmbeddings(ctx, st, provider, store.ListParams{
			Limit:          1000,
			IncludeDeleted: true,
			Lifecycle:      store.LifecycleAny,
		})
		if err != nil {
			return evalRunResult{}, err
		}
	}

	caseResults := make([]recoileval.CaseResult, 0, len(fixture.Cases))
	for _, tc := range fixture.Cases {
		start := time.Now()
		output, err := runEvalCase(ctx, st, seed, tc, retrieval, provider)
		elapsed := time.Since(start)
		if err != nil {
			return evalRunResult{}, fmt.Errorf("%s: %w", tc.ID, err)
		}
		caseResults = append(caseResults, recoileval.ScoreCase(tc, output.IDs, output.Texts, elapsed))
	}

	result := evalRunResult{
		FixturePath:          fixturePath,
		DBPath:               dbPath,
		TemporaryDB:          !evalOpts.keepDB,
		Retrieval:            retrieval,
		Indexed:              indexed,
		SeededMemories:       len(seed.fixtureToMemory),
		MinedChunks:          mined,
		SeededTranscripts:    seededTranscripts,
		MinedSessionEvidence: minedSessionEvidence,
		Summary:              recoileval.Summarize(caseResults),
		Cases:                caseResults,
	}
	if provider != nil {
		result.Provider = provider.Name()
		result.Model = provider.Model()
	}
	return result, nil
}

func evalSuiteFixturePaths(suite string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(suite)) {
	case "", "default":
		return []string{defaultEvalFixture}, nil
	case "embeddings":
		return []string{"eval/embeddings.jsonl"}, nil
	case "session-evidence":
		return []string{"eval/workflows/session-evidence.jsonl"}, nil
	case "cli-hard":
		return workflowFixturePaths()
	case "decisions":
		return decisionFixturePaths()
	case "workflows", "workflow":
		return workflowFixturePaths()
	default:
		return nil, fmt.Errorf("unknown --suite %q (want: default, embeddings, workflows, session-evidence, decisions, cli-hard)", suite)
	}
}

func decisionFixturePaths() ([]string, error) {
	paths := []string{
		"eval/workflows/decision-relevance.jsonl",
		"eval/workflows/decision-opposition.jsonl",
		"eval/workflows/predicate-coverage.jsonl",
		"eval/workflows/predicate-composition.jsonl",
		"eval/workflows/cross-domain-matching.jsonl",
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func workflowFixturePaths() ([]string, error) {
	paths, err := filepath.Glob("eval/workflows/*.jsonl")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no workflow fixtures found under eval/workflows")
	}
	return paths, nil
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
	if result.SeededTranscripts > 0 {
		meta = append(meta,
			kv{k: "seeded_transcripts", v: fmt.Sprintf("%d", result.SeededTranscripts)},
			kv{k: "mined_session_evidence", v: fmt.Sprintf("%d", result.MinedSessionEvidence)},
		)
	}
	meta = append(meta,
		kv{k: "case_count", v: fmt.Sprintf("%d", result.Summary.TotalCases)},
		kv{k: "passed", v: fmt.Sprintf("%d", result.Summary.PassedCases)},
		kv{k: "failed", v: fmt.Sprintf("%d", result.Summary.FailedCases)},
		kv{k: "recall_at_k", v: fmt.Sprintf("%.4f", result.Summary.RecallAtK)},
		kv{k: "content_hit_count", v: fmt.Sprintf("%d", result.Summary.ContentHitCount)},
		kv{k: "expected_content_count", v: fmt.Sprintf("%d", result.Summary.ExpectedContentCount)},
		kv{k: "mrr", v: fmt.Sprintf("%.4f", result.Summary.MRR)},
		kv{k: "empty_result_accuracy", v: fmt.Sprintf("%.4f", result.Summary.EmptyResultAccuracy)},
		kv{k: "scope_isolation_failures", v: fmt.Sprintf("%d", result.Summary.ScopeIsolationFailures)},
		kv{k: "stale_demotion_failures", v: fmt.Sprintf("%d", result.Summary.StaleDemotionFailures)},
		kv{k: "wake_safety_failures", v: fmt.Sprintf("%d", result.Summary.WakeSafetyFailures)},
		kv{k: "wrong_memory_failures", v: fmt.Sprintf("%d", result.Summary.WrongMemoryFailures)},
		kv{k: "ranked_content_failures", v: fmt.Sprintf("%d", result.Summary.RankedContentFailures)},
		kv{k: "latency_ms", v: fmt.Sprintf("%d", result.Summary.LatencyMS)},
	)
	return meta
}

func evalSuiteSummary(suite, retrieval string, fixtures []evalRunResult) evalSuiteResult {
	if strings.TrimSpace(suite) == "" {
		suite = "custom"
	}
	var cases []recoileval.CaseResult
	for _, fixture := range fixtures {
		cases = append(cases, fixture.Cases...)
	}
	return evalSuiteResult{
		Suite:     suite,
		Retrieval: retrieval,
		Summary:   recoileval.Summarize(cases),
		Fixtures:  fixtures,
		Cases:     cases,
	}
}

func evalSuiteFrontmatter(result evalSuiteResult) []kv {
	return []kv{
		{k: "suite", v: result.Suite},
		{k: "retrieval", v: result.Retrieval},
		{k: "fixture_count", v: fmt.Sprintf("%d", len(result.Fixtures))},
		{k: "case_count", v: fmt.Sprintf("%d", result.Summary.TotalCases)},
		{k: "passed", v: fmt.Sprintf("%d", result.Summary.PassedCases)},
		{k: "failed", v: fmt.Sprintf("%d", result.Summary.FailedCases)},
		{k: "recall_at_k", v: fmt.Sprintf("%.4f", result.Summary.RecallAtK)},
		{k: "content_hit_count", v: fmt.Sprintf("%d", result.Summary.ContentHitCount)},
		{k: "expected_content_count", v: fmt.Sprintf("%d", result.Summary.ExpectedContentCount)},
		{k: "mrr", v: fmt.Sprintf("%.4f", result.Summary.MRR)},
		{k: "empty_result_accuracy", v: fmt.Sprintf("%.4f", result.Summary.EmptyResultAccuracy)},
		{k: "scope_isolation_failures", v: fmt.Sprintf("%d", result.Summary.ScopeIsolationFailures)},
		{k: "stale_demotion_failures", v: fmt.Sprintf("%d", result.Summary.StaleDemotionFailures)},
		{k: "wake_safety_failures", v: fmt.Sprintf("%d", result.Summary.WakeSafetyFailures)},
		{k: "wrong_memory_failures", v: fmt.Sprintf("%d", result.Summary.WrongMemoryFailures)},
		{k: "ranked_content_failures", v: fmt.Sprintf("%d", result.Summary.RankedContentFailures)},
		{k: "latency_ms", v: fmt.Sprintf("%d", result.Summary.LatencyMS)},
	}
}

func evalFailureError(summary recoileval.Summary) error {
	if summary.FailedCases == 0 {
		return nil
	}
	return fmt.Errorf("eval failed: %d of %d cases failed", summary.FailedCases, summary.TotalCases)
}

func writeEvalArtifacts(outPath, kind, name string, payload any, meta []kv, body string) error {
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		return nil
	}
	jsonPath, markdownPath, err := evalArtifactPaths(outPath, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(markdownPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(envelope{Version: "0.1", Kind: kind, Data: payload}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(markdownPath, []byte(renderFrontmatter(meta, body)), 0o644)
}

func evalArtifactPaths(outPath, name string) (string, string, error) {
	if info, err := os.Stat(outPath); err == nil && info.IsDir() {
		base := evalArtifactName(name)
		return filepath.Join(outPath, base+".json"), filepath.Join(outPath, base+".md"), nil
	}
	ext := strings.ToLower(filepath.Ext(outPath))
	switch ext {
	case "":
		base := evalArtifactName(name)
		return filepath.Join(outPath, base+".json"), filepath.Join(outPath, base+".md"), nil
	case ".json":
		stem := strings.TrimSuffix(outPath, ext)
		return outPath, stem + ".md", nil
	case ".md":
		stem := strings.TrimSuffix(outPath, ext)
		return stem + ".json", outPath, nil
	default:
		return "", "", fmt.Errorf("--out must be a directory, .json file, or .md file")
	}
}

func evalRunArtifactName(result evalRunResult) string {
	base := filepath.Base(result.FixturePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return "eval_" + evalArtifactName(base)
}

func evalSuiteArtifactName(result evalSuiteResult) string {
	return "eval_" + evalArtifactName(result.Suite)
}

func evalArtifactName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "results"
	}
	return out
}

func renderFrontmatter(meta []kv, body string) string {
	var b bytes.Buffer
	_ = frontmatter(&b, meta, body)
	return b.String()
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
			SourceKind:   rec.SourceKind,
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
	metadata := make(map[string]any, len(rec.Metadata)+2)
	for k, v := range rec.Metadata {
		if k == predicateMetadataKey && strings.HasPrefix(strings.TrimSpace(v), "{") {
			var pred any
			if err := json.Unmarshal([]byte(v), &pred); err != nil {
				return "", fmt.Errorf("fixture %s predicate metadata: %w", rec.ID, err)
			}
			metadata[k] = pred
			continue
		}
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

func runEvalCase(ctx context.Context, st *store.Store, seed evalSeed, tc recoileval.CaseRecord, retrieval string, provider embedding.Provider) (evalCaseOutput, error) {
	limit := evalLimit(tc)
	switch tc.Mode {
	case "search":
		results, err := runEvalSearch(ctx, st, provider, retrieval, store.SearchParams{
			Query:        tc.Query,
			ScopeKind:    tc.Scope.Kind,
			ScopeID:      tc.Scope.ID,
			Role:         tc.Role,
			ClaimKey:     tc.ClaimKey,
			Validity:     tc.Validity,
			SourceAgent:  tc.SourceAgent,
			SourcePath:   tc.SourcePath,
			Limit:        limit,
			Lifecycle:    evalLifecycle(tc, store.LifecycleCurrent),
			SignalRerank: true,
		})
		if err != nil {
			return evalCaseOutput{}, err
		}
		return evalCaseOutput{IDs: evalResultKeys(seed, tc, results), Texts: evalResultTexts(results)}, nil
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
			return evalCaseOutput{}, err
		}
		return evalCaseOutput{IDs: evalResultKeys(seed, tc, results), Texts: evalResultTexts(results)}, nil
	case "wake":
		fetchLimit := wakeFetchLimit(limit)
		var queryResults []store.Memory
		if strings.TrimSpace(tc.Query) != "" {
			results, err := runEvalSearch(ctx, st, provider, retrieval, store.SearchParams{
				Query:        tc.Query,
				ScopeKind:    tc.Scope.Kind,
				ScopeID:      tc.Scope.ID,
				Role:         tc.Role,
				ClaimKey:     tc.ClaimKey,
				Validity:     tc.Validity,
				SourceAgent:  tc.SourceAgent,
				SourcePath:   tc.SourcePath,
				Limit:        fetchLimit,
				Lifecycle:    evalLifecycle(tc, store.LifecycleCurrent),
				SignalRerank: true,
			})
			if err != nil {
				return evalCaseOutput{}, err
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
			return evalCaseOutput{}, err
		}
		layers := buildWakeLayers(tc.Query, queryResults, recent, limit)
		memories := flattenWakeLayers(layers)
		texts := evalResultTexts(memories)
		if tc.IncludeDecisions {
			trail, err := decisionTrail(ctx, st, runtimeScopeFromEval(tc.Scope), limit)
			if err != nil {
				return evalCaseOutput{}, err
			}
			texts = append([]string{renderDecisionTrail(trail)}, texts...)
		}
		return evalCaseOutput{IDs: evalResultKeys(seed, tc, memories), Texts: texts}, nil
	case "check":
		result, err := runDecisionCheck(ctx, st, runtimeScopeFromEval(tc.Scope), tc.Query, tc.ClaimKey, limit)
		if err != nil {
			return evalCaseOutput{}, err
		}
		memories := checkResultMemories(result)
		return evalCaseOutput{IDs: evalResultKeys(seed, tc, memories), Texts: []string{renderCheckTextForEval(result)}}, nil
	default:
		return evalCaseOutput{}, fmt.Errorf("unsupported mode %q", tc.Mode)
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
	return runRetriever(ctx, st, params, retrieverOptions{
		mode:       retrieval,
		provider:   provider,
		hybridPool: maxInt(params.Limit*4, 20),
		fusionK:    60,
		limit:      params.Limit,
	})
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

func evalResultText(memories []store.Memory) string {
	return strings.Join(evalResultTexts(memories), "\n")
}

func checkResultMemories(result checkResult) []store.Memory {
	var memories []store.Memory
	seen := map[string]bool{}
	add := func(mem *store.Memory) {
		if mem == nil || seen[mem.ID] {
			return
		}
		seen[mem.ID] = true
		memories = append(memories, *mem)
	}
	add(result.MatchedMemory)
	add(result.CurrentDecision)
	add(result.Replacement)
	for i := range result.Family {
		add(&result.Family[i])
	}
	return memories
}

func renderCheckTextForEval(result checkResult) string {
	var b strings.Builder
	for _, item := range checkFrontmatter(result) {
		if item.v != "" {
			fmt.Fprintf(&b, "%s: %s\n", item.k, item.v)
		}
	}
	b.WriteString(renderCheckResult(result))
	return b.String()
}

func evalResultTexts(memories []store.Memory) []string {
	texts := make([]string, 0, len(memories))
	for _, mem := range memories {
		texts = append(texts, evalMemoryText(mem))
	}
	return texts
}

func evalMemoryText(mem store.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
		mem.Content,
		mem.Role,
		mem.SourceKind,
		mem.SourceAgent,
		mem.SourcePath,
		mem.SourceRef,
		mem.ClaimKey,
		mem.MetadataJSON,
	)
	return b.String()
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

func seedEvalTranscripts(ctx context.Context, st *store.Store, fixturePath string, fixture recoileval.Fixture) (int, int, error) {
	if len(fixture.Transcripts) == 0 {
		return 0, 0, nil
	}
	stateDir, err := config.ResolveStateDir()
	if err != nil {
		return 0, 0, err
	}
	fixtureDir := filepath.Dir(fixturePath)
	totalMined := 0
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for i, transcript := range fixture.Transcripts {
		path := transcript.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(fixtureDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, totalMined, fmt.Errorf("transcript %s: %w", transcript.ID, err)
		}
		agent := transcript.SourceAgent
		if agent == "" {
			agent = "recoil-eval"
		}
		result, err := sessionevidence.Ingest(data, sessionevidence.Options{
			StateDir:    stateDir,
			ScopeKind:   transcript.Scope.Kind,
			ScopeID:     transcript.Scope.ID,
			SourceAgent: agent,
			SessionID:   transcript.SessionID,
			MinChars:    transcript.MinChars,
			Now:         base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			return 0, totalMined, fmt.Errorf("transcript %s ingest: %w", transcript.ID, err)
		}
		if result.Selected == 0 || result.SourcePath == "" {
			continue
		}
		sc := runtimeScopeFromEval(transcript.Scope)
		mined, err := mineSessionEvidence(ctx, st, sc, mineOptions{agent: agent}, result.SourcePath)
		if err != nil {
			return 0, totalMined, fmt.Errorf("transcript %s mine: %w", transcript.ID, err)
		}
		totalMined += mined.Added
	}
	return len(fixture.Transcripts), totalMined, nil
}

func runtimeScopeFromEval(sc recoileval.Scope) scope.Scope {
	out := scope.Scope{Kind: sc.Kind, ID: sc.ID}
	switch sc.Kind {
	case "project":
		out.ProjectID = sc.ID
	case "session":
		out.SessionID = sc.ID
	}
	return out
}

func isolateEvalStateDir(root string) func() {
	key := "XDG_CONFIG_HOME"
	old, hadOld := os.LookupEnv(key)
	_ = os.Setenv(key, filepath.Join(root, "xdg-config"))
	return func() {
		if hadOld {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
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
		writeEvalIDs(&b, "expected_content", result.ExpectedContent)
		writeEvalIDs(&b, "missing_current", result.MissingCurrentIDs)
		writeEvalIDs(&b, "forbidden_present", result.ForbiddenIDsPresent)
		writeEvalIDs(&b, "forbidden_current_present", result.ForbiddenCurrentIDsPresent)
		writeEvalIDs(&b, "missing_content", result.MissingContent)
		writeEvalIDs(&b, "missing_ranked_content", result.MissingRankedContent)
		writeEvalIDs(&b, "forbidden_content_present", result.ForbiddenContentPresent)
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
