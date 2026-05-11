package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	recoileval "github.com/1broseidon/recoil/internal/eval"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

const defaultEvalFixture = "eval/fixtures.jsonl"

type evalOptions struct {
	keepDB bool
}

type evalRunResult struct {
	FixturePath    string                  `json:"fixture_path"`
	DBPath         string                  `json:"db_path"`
	TemporaryDB    bool                    `json:"temporary_db"`
	SeededMemories int                     `json:"seeded_memories"`
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

			caseResults := make([]recoileval.CaseResult, 0, len(fixture.Cases))
			for _, tc := range fixture.Cases {
				start := time.Now()
				resultIDs, err := runEvalCase(ctx, st, seed, tc)
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
				SeededMemories: len(seed.fixtureToMemory),
				Summary:        recoileval.Summarize(caseResults),
				Cases:          caseResults,
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, result)
			}
			return frontmatter(w, []kv{
				{k: "fixture", v: fixturePath},
				{k: "db_path", v: dbPath},
				{k: "temporary_db", v: fmt.Sprintf("%t", result.TemporaryDB)},
				{k: "seeded_memories", v: fmt.Sprintf("%d", result.SeededMemories)},
				{k: "case_count", v: fmt.Sprintf("%d", result.Summary.TotalCases)},
				{k: "passed", v: fmt.Sprintf("%d", result.Summary.PassedCases)},
				{k: "failed", v: fmt.Sprintf("%d", result.Summary.FailedCases)},
				{k: "recall_at_k", v: fmt.Sprintf("%.4f", result.Summary.RecallAtK)},
				{k: "mrr", v: fmt.Sprintf("%.4f", result.Summary.MRR)},
				{k: "empty_result_accuracy", v: fmt.Sprintf("%.4f", result.Summary.EmptyResultAccuracy)},
				{k: "scope_isolation_failures", v: fmt.Sprintf("%d", result.Summary.ScopeIsolationFailures)},
				{k: "stale_demotion_failures", v: fmt.Sprintf("%d", result.Summary.StaleDemotionFailures)},
				{k: "wake_safety_failures", v: fmt.Sprintf("%d", result.Summary.WakeSafetyFailures)},
				{k: "latency_ms", v: fmt.Sprintf("%d", result.Summary.LatencyMS)},
			}, evalResultLines(result.Cases))
		},
	}
	c.Flags().BoolVar(&evalOpts.keepDB, "keep-db", false, "keep the temporary eval database after the run")
	return c
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

func runEvalCase(ctx context.Context, st *store.Store, seed evalSeed, tc recoileval.CaseRecord) ([]string, error) {
	limit := evalLimit(tc)
	switch tc.Mode {
	case "search":
		results, err := st.Search(ctx, store.SearchParams{
			Query:     tc.Query,
			ScopeKind: tc.Scope.Kind,
			ScopeID:   tc.Scope.ID,
			Limit:     limit,
		})
		if err != nil {
			return nil, err
		}
		current, _ := splitCurrentHistorical(results)
		return fixtureIDsFor(seed, limitMemories(current, limit)), nil
	case "wake":
		fetchLimit := wakeFetchLimit(limit)
		var queryResults []store.Memory
		if strings.TrimSpace(tc.Query) != "" {
			results, err := st.Search(ctx, store.SearchParams{
				Query:     tc.Query,
				ScopeKind: tc.Scope.Kind,
				ScopeID:   tc.Scope.ID,
				Limit:     fetchLimit,
			})
			if err != nil {
				return nil, err
			}
			queryResults = results
		}
		recent, err := st.List(ctx, store.ListParams{
			ScopeKind: tc.Scope.Kind,
			ScopeID:   tc.Scope.ID,
			Limit:     fetchLimit,
		})
		if err != nil {
			return nil, err
		}
		layers := buildWakeLayers(tc.Query, queryResults, recent, limit)
		return fixtureIDsFor(seed, flattenWakeLayers(layers)), nil
	default:
		return nil, fmt.Errorf("unsupported mode %q", tc.Mode)
	}
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
