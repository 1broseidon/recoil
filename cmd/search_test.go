package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/sourcequality"
	"github.com/1broseidon/recoil/internal/store"
)

func TestRunSearchAutoModeFallsBackToFTSWithoutEmbeddingIndex(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "auto retrieval sparse fallback target", ScopeKind: "session", ScopeID: "auto-fts", Validity: "active"}); err != nil {
		t.Fatal(err)
	}
	result, err := runSearch(ctx, st, scope.Scope{Kind: "session", ID: "auto-fts"}, "sparse fallback", searchOptions{limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrievalMode != retrievalFTS || result.ResultCount == 0 {
		t.Fatalf("expected fts results, got mode=%q count=%d", result.RetrievalMode, result.ResultCount)
	}
	if !frontmatterContains(searchFrontmatter(result), "retrieval_mode", retrievalFTS) {
		t.Fatalf("missing retrieval_mode frontmatter: %+v", searchFrontmatter(result))
	}
}

func TestRunSearchAutoModeUsesUsableEmbeddingIndex(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	provider := embedding.NewLocalProvider("")
	for i := 0; i < embeddingIndexFloor; i++ {
		mem, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "note", Content: fmt.Sprintf("hybrid indexed target %02d", i), ScopeKind: "session", ScopeID: "auto-hybrid", Validity: "active"})
		if err != nil {
			t.Fatal(err)
		}
		vector, _ := provider.Embed(ctx, store.EmbeddingText(*mem))
		if err := st.UpsertEmbedding(ctx, *mem, provider.Name(), provider.Model(), vector); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runSearch(ctx, st, scope.Scope{Kind: "session", ID: "auto-hybrid"}, "hybrid indexed target", searchOptions{limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrievalMode != retrievalHybrid || result.ResultCount == 0 {
		t.Fatalf("expected hybrid results, got mode=%q count=%d", result.RetrievalMode, result.ResultCount)
	}
}

func TestRunSearchAutoHybridQueryEmbedErrorFallsBackToFTS(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "query embed failure fallback target", ScopeKind: "session", ScopeID: "fallback", Validity: "active"}); err != nil {
		t.Fatal(err)
	}
	oldResolver := activeSearchModeResolver
	activeSearchModeResolver = func(context.Context, *store.Store, scope.Scope, config.Settings, bool, string, string) (string, embedding.Provider, bool, error) {
		return retrievalHybrid, failingEmbeddingProvider{}, true, nil
	}
	defer func() { activeSearchModeResolver = oldResolver }()
	result, err := runSearch(ctx, st, scope.Scope{Kind: "session", ID: "fallback"}, "failure fallback", searchOptions{limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrievalMode != retrievalHybridFallbackFTS || result.ResultCount == 0 {
		t.Fatalf("expected hybrid fallback fts results, got mode=%q count=%d", result.RetrievalMode, result.ResultCount)
	}
}

type failingEmbeddingProvider struct{}

func (failingEmbeddingProvider) Name() string  { return "local" }
func (failingEmbeddingProvider) Model() string { return "local-hash-v1" }
func (failingEmbeddingProvider) Embed(context.Context, string) ([]float64, error) {
	return nil, fmt.Errorf("boom")
}

func frontmatterContains(meta []kv, key, value string) bool {
	for _, item := range meta {
		if item.k == key && item.v == value {
			return true
		}
	}
	return false
}

func TestSearchCommandExplainIncludesScoreComponents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(ctx, store.AddMemoryParams{
		Role:       "decision",
		Content:    "Explain target keeps sqlite FTS scoring visible.",
		SourceKind: "direct",
		ScopeKind:  "session",
		ScopeID:    "explain-search",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newSearchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "explain-search", "--explain", "explain target sqlite"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"explain:", "final_score", "retrieval_mode", "token_coverage"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in explain output:\n%s", want, got)
		}
	}
}

func TestSearchCommandSeparatesCurrentAndHistoricalResults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Pure Go sqlite was rejected because FTS5 support is required.",
		ScopeKind: "session",
		ScopeID:   "project-1",
		Validity:  "rejected",
		ClaimKey:  "dependency.sqlite-driver",
	})
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "adr",
		Content:   "Current sqlite driver is mattn go sqlite3 with FTS5.",
		ScopeKind: "session",
		ScopeID:   "project-1",
		Validity:  "active",
		ClaimKey:  "dependency.sqlite-driver",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newSearchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "project-1", "sqlite FTS5"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"result_count: 1",
		"history_count: 1",
		"## Current Decisions",
		"## Historical",
		"why: current adr with claim_key dependency.sqlite-driver",
		active.ID,
		"validity: rejected",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestSearchCommandCurrentResultsSurviveHistoricalCrowding(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	defer func() { opts = oldOpts }()

	ctx := context.Background()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:      "decision",
			Content:   fmt.Sprintf("Crowded search phrase rejected history %02d repeated repeated repeated.", i),
			ScopeKind: "session",
			ScopeID:   "crowded-search",
			Validity:  "rejected",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	active, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "adr",
		Content:   "Crowded search phrase active canonical guidance.",
		ScopeKind: "session",
		ScopeID:   "crowded-search",
		Validity:  "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newSearchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--session", "crowded-search", "--limit", "5", "crowded search phrase"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"result_count: 1",
		"history_count: 5",
		active.ID,
		"## Current Decisions",
		"## Historical",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestStructuredRetrievalLanesKeepSessionEvidenceOutOfCurrentDecisions(t *testing.T) {
	lanes := structuredRetrievalLanes("auth", []store.Memory{
		{ID: "mem_session_decision", Role: "decision", SourceKind: "session_evidence", Content: "previous session said use bearer auth", MetadataJSON: `{"kind":"session_evidence","branch":"main","turn_start":4,"turn_end":5}`},
		{ID: "mem_direct_decision", Role: "decision", SourceKind: "direct", Content: "Current decision: use mTLS."},
	}, nil)
	byID := map[string]string{}
	for _, lane := range lanes {
		for _, mem := range lane.Results {
			byID[mem.ID] = lane.Key
		}
	}
	if byID["mem_session_decision"] != "recent_evidence" {
		t.Fatalf("expected session evidence in recent_evidence lane, got lanes %+v", lanes)
	}
	if byID["mem_direct_decision"] != "current_decisions" {
		t.Fatalf("expected direct decision in current_decisions lane, got lanes %+v", lanes)
	}
}

func TestRetrievalBlocksRenderSessionEvidenceProvenance(t *testing.T) {
	lanes := []retrievalLaneResult{{Key: "recent_evidence", Title: "Recent Evidence", Results: []store.Memory{{
		ID:           "mem_session",
		Role:         "source",
		Content:      "user: go with bearer auth",
		SourceKind:   "session_evidence",
		SourceAgent:  "codex",
		SourceRef:    "turn 4-5",
		SessionID:    "sess-123456789abcdef",
		CreatedAt:    "2026-05-11T12:00:00Z",
		MetadataJSON: `{"kind":"session_evidence","native_session_id":"native-abcdef123456789","branch":"main","turn_start":4,"turn_end":5}`,
	}}}}
	got := retrievalLaneBlocks(lanes, 0)
	for _, want := range []string{"source_kind: session_evidence", "agent: codex", "session_id: sess-123456789abcdef", "session_short_id: sess-1234567", "native_session_id: native-abcdef123456789", "branch: main", "source_ref: turn 4-5", "turn_range: 4-5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in rendered session provenance:\n%s", want, got)
		}
	}
}

func TestRunSignalSearchAppliesAgingPenaltyOnlyToAgingRoles(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	oldNote, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "note", Content: "aging rank target note old", ScopeKind: "session", ScopeID: "aging-search", Validity: "active", CreatedAt: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	freshNote, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "note", Content: "aging rank target note fresh", ScopeKind: "session", ScopeID: "aging-search", Validity: "active", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	oldDecision, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "aging rank target decision old", ScopeKind: "session", ScopeID: "aging-search", Validity: "active", CreatedAt: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{Query: "aging rank target note", ScopeKind: "session", ScopeID: "aging-search", Limit: 3, Lifecycle: store.LifecycleCurrent, SignalRerank: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].ID != freshNote.ID {
		t.Fatalf("expected fresh aging-role memory first, got %+v", results)
	}
	if agingPenalty(*oldNote, time.Now().UTC(), defaultAgingWindowDays) <= 0 {
		t.Fatalf("expected old note to receive aging penalty")
	}
	if agingPenalty(*oldDecision, time.Now().UTC(), defaultAgingWindowDays) != 0 {
		t.Fatalf("decision should not be penalized by age")
	}
}

func TestSearchRoutesExpiredValidUntilToHistoricalLane(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	expired, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "predicate lane expired libz decision", ScopeKind: "session", ScopeID: "predicate-search", Validity: "active", MetadataJSON: `{"predicate":{"kind":"valid_until","valid_until":"2020-01-01","recheck_prompt":"Recheck libz"}}`})
	if err != nil {
		t.Fatal(err)
	}
	future, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "predicate lane future libz decision", ScopeKind: "session", ScopeID: "predicate-search", Validity: "active", MetadataJSON: `{"predicate":{"kind":"valid_until","valid_until":"2999-01-01","recheck_prompt":"Recheck libz"}}`})
	if err != nil {
		t.Fatal(err)
	}
	current, err := runSignalSearch(ctx, st, store.SearchParams{Query: "predicate lane libz decision", ScopeKind: "session", ScopeID: "predicate-search", Limit: 5, Lifecycle: store.LifecycleCurrent, SignalRerank: true})
	if err != nil {
		t.Fatal(err)
	}
	lanes := structuredRetrievalLanes("predicate lane libz decision", current, nil)
	var sawExpiredHistorical, sawFutureCurrent bool
	for _, lane := range lanes {
		for _, mem := range lane.Results {
			if mem.ID == expired.ID && lane.Key == "historical" && strings.Contains(mem.Why, "valid_until expired") && strings.Contains(mem.Why, "Recheck libz") {
				sawExpiredHistorical = true
			}
			if mem.ID == future.ID && lane.Key != "historical" {
				sawFutureCurrent = true
			}
		}
	}
	if !sawExpiredHistorical || !sawFutureCurrent {
		t.Fatalf("expected expired historical and future current, lanes=%+v", lanes)
	}
}

func TestRunSignalSearchFiltersAbsentFactLeakage(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Weak chunk: shares only incidental tokens ("clients") with the query.
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:       "source",
		SourceKind: "file",
		SourcePath: "docs/roadmap.md",
		Content:    "clients read the deployment roadmap and documented milestones",
		ScopeKind:  "session",
		ScopeID:    "absent-leak",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}
	// Negated mention: says there is no redis — evidence of absence, not an answer.
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:       "source",
		SourceKind: "file",
		SourcePath: "docs/storage.md",
		Content:    "There is no redis in the stack; storage uses sqlite only.",
		ScopeKind:  "session",
		ScopeID:    "absent-leak",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "what graphql schema do we expose to clients",
		ScopeKind:    "session",
		ScopeID:      "absent-leak",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected absent-fact query to return empty, got %+v", results)
	}

	results, err = runSignalSearch(ctx, st, store.SearchParams{
		Query:        "what redis configuration do we use",
		ScopeKind:    "session",
		ScopeID:      "absent-leak",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mem := range results {
		if strings.Contains(mem.Content, "no redis") {
			t.Fatalf("negated mention surfaced as answer: %+v", mem)
		}
	}
}

func TestRunSignalSearchKeepsGuidanceForNegatedSubjectQueries(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// A rejection decision must keep surfacing for queries about its subject.
	decision, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:      "decision",
		Content:   "Decided against redis for the cache; do not use redis while ops cost is unjustified.",
		ScopeKind: "session",
		ScopeID:   "guidance-negation",
		Validity:  "active",
		ClaimKey:  "dependency.cache.redis",
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "should we use redis for caching",
		ScopeKind:    "session",
		ScopeID:      "guidance-negation",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != decision.ID {
		t.Fatalf("expected rejection decision to surface for subject query, got %+v", results)
	}
}

func TestRunSignalSearchFusesQueryVariants(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	lee, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy.",
		SourcePath: "sessions/health.jsonl",
		ScopeKind:  "session",
		ScopeID:    "query-variants",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	smith, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Dr. Smith prescribed antibiotics after the urinary tract infection.",
		SourcePath: "sessions/uti.jsonl",
		ScopeKind:  "session",
		ScopeID:    "query-variants",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Which doctor discussed biopsy and which doctor handled the UTI?",
		ScopeKind:    "session",
		ScopeID:      "query-variants",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !containsMemoryID(results, lee.ID) || !containsMemoryID(results, smith.ID) {
		t.Fatalf("expected variant fusion to retrieve both doctor memories, got %+v", results)
	}
}

func TestRunSignalSearchDiversifiesSources(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for i := range 3 {
		if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Content:    fmt.Sprintf("sqlite FTS5 search ranking canonical repeated note %d", i),
			SourcePath: "docs/main.md",
			ScopeKind:  "session",
			ScopeID:    "diversity",
			Validity:   "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	other, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "search ranking caveat from release notes",
		SourcePath: "docs/release.md",
		ScopeKind:  "session",
		ScopeID:    "diversity",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "sqlite FTS5 and search ranking",
		ScopeKind:    "session",
		ScopeID:      "diversity",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || !containsMemoryID(results, other.ID) {
		t.Fatalf("expected diversified results to include second source, got %+v", results)
	}
}

func TestRunSignalSearchPrioritizesOperationalDocsForBroadAgentQueries(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	agentDoc, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "This file provides guidance to AI agents before editing this repository. Run focused tests and follow local conventions.",
		SourceKind: "file",
		SourcePath: "AGENTS.md",
		ScopeKind:  "session",
		ScopeID:    "source-priors",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "An example application README for agents before editing setup tests repository guidance.",
		SourceKind: "file",
		SourcePath: "examples/demo/README.md",
		ScopeKind:  "session",
		ScopeID:    "source-priors",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "what should agents know before editing this repo",
		ScopeKind:    "session",
		ScopeID:      "source-priors",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != agentDoc.ID {
		t.Fatalf("expected AGENTS.md first for broad agent query, got %+v", results)
	}
}

func TestSearchPrioritizesSecurityDisclosureDocs(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	securityDoc, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Security Policy. Reporting a vulnerability uses the private disclosure process.",
		SourceKind: "file",
		SourcePath: ".github/SECURITY.md",
		ScopeKind:  "session",
		ScopeID:    "security-priors",
		Validity:   "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Example app security policy rows for database authorization and vulnerability-shaped test content.",
		SourceKind: "file",
		SourcePath: "examples/auth/README.md",
		ScopeKind:  "session",
		ScopeID:    "security-priors",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := st.Search(ctx, store.SearchParams{
		Query:        "security policy vulnerability disclosure",
		ScopeKind:    "session",
		ScopeID:      "security-priors",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != securityDoc.ID {
		t.Fatalf("expected security policy first, got %+v", results)
	}
}

func TestRunSignalSearchHonorsExplicitDoctorName(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy.",
		SourcePath: "sessions/health.jsonl",
		ScopeKind:  "session",
		ScopeID:    "doctor-name",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:    "Dr. Smith prescribed antibiotics after the urinary tract infection.",
		SourcePath: "sessions/uti.jsonl",
		ScopeKind:  "session",
		ScopeID:    "doctor-name",
		Validity:   "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Dr. Alvarez cardiologist appointment",
		ScopeKind:    "session",
		ScopeID:      "doctor-name",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no unrelated doctor results for explicit Dr. Alvarez query, got %+v", results)
	}
}

func TestRunSignalSearchProductBigramDoesNotEmptyResults(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Editor tasks run through the Makefile; agents adopt the standard build tasks.",
		Role:      "decision",
		ScopeKind: "session",
		ScopeID:   "product-bigram",
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "should we adopt Visual Studio Code tasks",
		ScopeKind:    "session",
		ScopeID:      "product-bigram",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected product-name bigram query to keep matching results, got none")
	}
}

func TestRunSignalSearchDemotesButKeepsNonMatchingForPersonQueries(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "Caroline Mercer recommended the sourdough cookbook for weekend baking.",
		ScopeKind: "session",
		ScopeID:   "person-demote",
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Content:   "A colleague recommended a different cookbook about weeknight cooking.",
		ScopeKind: "session",
		ScopeID:   "person-demote",
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "which cookbook did my friend Caroline Mercer recommend",
		ScopeKind:    "session",
		ScopeID:      "person-demote",
		Limit:        5,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected non-matching memory demoted but kept, got %d results", len(results))
	}
	if !strings.Contains(results[0].Content, "Caroline Mercer") {
		t.Fatalf("expected Caroline Mercer memory ranked first, got %q", results[0].Content)
	}
}

func TestExplicitPersonNameTermsRequiresPersonContext(t *testing.T) {
	if names := explicitPersonNameTerms("migrate the schema to North Star conventions"); len(names) != 0 {
		t.Fatalf("expected no person names without person context, got %v", names)
	}
	if names := explicitPersonNameTerms("what did my friend Caroline Mercer recommend"); len(names) == 0 {
		t.Fatal("expected person names with person cue present")
	}
}

func TestRunSignalSearchExpandsDerivedTraceToSourceEvidence(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	raw, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "source",
		Content:      "user: I prefer deep learning for medical image analysis.",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/profile.jsonl",
		SourceRef:    "turn 1-2",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		SessionID:    "sess-profile",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	derived, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "trace",
		Content:      "Derived user profile trace: user_interest publications conferences deep medical image analysis",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/profile.jsonl",
		SourceRef:    "turn 1-2 profile",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		SessionID:    "sess-profile",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence","derived_type":"derived_profile"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "publications conferences medical imaging",
		ScopeKind:    "session",
		ScopeID:      "derived-parent",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != derived.ID || results[1].ID != raw.ID {
		t.Fatalf("expected derived trace followed by raw parent evidence, got %+v", results)
	}
}

func TestRunSignalSearchExpandsSourceEvidenceToDerivedChildren(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	raw, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "source",
		Content:      "user: Switch to mattn/go-sqlite3 now; no longer use modernc.",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/update.jsonl",
		SourceRef:    "turn 5-6",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		SessionID:    "sess-update",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := st.AddMemory(ctx, store.AddMemoryParams{
		Role:         "trace",
		Content:      "Derived update trace: current_state latest_update sqlite mattn modernc",
		SourceKind:   "session_evidence",
		SourcePath:   "sessions/update.jsonl",
		SourceRef:    "turn 5-6 update",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		SessionID:    "sess-update",
		Validity:     "active",
		MetadataJSON: `{"kind":"session_evidence","derived_type":"derived_update"}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{
		Query:        "Switch to mattn go sqlite3",
		ScopeKind:    "session",
		ScopeID:      "derived-child",
		Role:         "source",
		Limit:        2,
		Lifecycle:    store.LifecycleCurrent,
		SignalRerank: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ID != raw.ID || results[1].ID != child.ID {
		t.Fatalf("expected raw evidence followed by derived child trace, got %+v", results)
	}
}

func TestIsNegativeEvidenceUsesNoiseMetadataNotHostedSyncText(t *testing.T) {
	metadataNoise := store.Memory{
		Content:      "Current architecture decision about local-first daemon behavior.",
		MetadataJSON: `{"noise":true}`,
	}
	if !isNegativeEvidence(metadataNoise) {
		t.Fatalf("expected noise metadata to mark negative evidence")
	}

	hostedSync := store.Memory{
		Content: "our hosted sync service launches Tuesday",
	}
	if isNegativeEvidence(hostedSync) {
		t.Fatalf("hosted sync phrasing alone should not mark negative evidence")
	}
}

func TestRunSignalSearchAppliesRecencyPriorToDirectMemories(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	oldDecision, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "recency rank target direct decision equal shape alpha", SourceKind: "direct", ScopeKind: "session", ScopeID: "recency-search", Validity: "active", CreatedAt: now.AddDate(0, 0, -60).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	freshDecision, _, err := st.AddMemory(ctx, store.AddMemoryParams{Role: "decision", Content: "recency rank target direct decision equal shape beta", SourceKind: "direct", ScopeKind: "session", ScopeID: "recency-search", Validity: "active", CreatedAt: now.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}

	results, err := runSignalSearch(ctx, st, store.SearchParams{Query: "recency rank target direct decision equal shape", ScopeKind: "session", ScopeID: "recency-search", Limit: 2, Lifecycle: store.LifecycleCurrent, SignalRerank: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].ID != freshDecision.ID || results[1].ID != oldDecision.ID {
		t.Fatalf("expected fresh direct decision first, got %+v", results)
	}
}

func TestExplainMemoryOmitsRecencyPriorForFileChunks(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	components := explainMemory(store.Memory{SourceKind: "file", CreatedAt: now.Format(time.RFC3339)}, "", sourcequality.Options{}, sourcequality.ModeSearch, defaultAgingWindowDays, "test", now)
	for _, component := range components {
		if component.Name == "recency_prior" {
			t.Fatalf("file chunk should not receive recency_prior component: %+v", components)
		}
	}
}

func containsMemoryID(memories []store.Memory, id string) bool {
	for _, mem := range memories {
		if mem.ID == id {
			return true
		}
	}
	return false
}
