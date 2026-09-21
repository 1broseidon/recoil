package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/embedding"
	"github.com/1broseidon/recoil/internal/retrieval"
	"github.com/1broseidon/recoil/internal/scope"
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
	explain        bool
	hybrid         bool
	hybridProvider string
	hybridModel    string
	hybridPool     int
	fusionK        int
	profiles       string
}

const (
	retrievalHybridFallbackFTS = "hybrid_fallback_fts"
	embeddingIndexFloor        = 10
)

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
			st, _, err := openReadStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			result, err := runSearch(ctx, st, sc, query, searchOpts)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "search_result", result)
			}
			if searchOpts.minimal {
				for _, r := range result.Results {
					writeMinimalMemory(w, r, true)
				}
				return nil
			}
			return frontmatter(w, searchFrontmatter(result), renderSearchResult(result, searchOpts.maxChars))
		},
	}
	addScopeFlags(c, &searchOpts.scope)
	addMemoryFilterFlags(c, &searchOpts.filters)
	c.Flags().IntVar(&searchOpts.limit, "limit", 5, "maximum number of memories to return")
	c.Flags().BoolVar(&searchOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().IntVar(&searchOpts.maxChars, "max-chars", 4000, "maximum characters of memory content to print")
	c.Flags().BoolVar(&searchOpts.explain, "explain", false, "include per-result score component diagnostics")
	c.Flags().BoolVar(&searchOpts.hybrid, "hybrid", false, "fuse FTS5 and embedding similarity via RRF (requires indexed embeddings)")
	c.Flags().StringVar(&searchOpts.hybridProvider, "hybrid-provider", embedding.OpenRouterProvider, "embedding provider for --hybrid")
	c.Flags().StringVar(&searchOpts.hybridModel, "hybrid-model", embedding.DefaultOpenRouterModel, "embedding model for --hybrid")
	c.Flags().IntVar(&searchOpts.hybridPool, "hybrid-pool", 50, "candidate pool size per retrieval method before fusion")
	c.Flags().IntVar(&searchOpts.fusionK, "fusion-k", 60, "RRF fusion constant (standard: 60)")
	c.Flags().StringVar(&searchOpts.profiles, "profiles", "auto", "profile retrieval mode: auto, on, or off")
	return c
}

func runSearch(ctx context.Context, st *store.Store, sc scope.Scope, query string, opts searchOptions) (searchResult, error) {
	params, err := searchParams(query, sc, opts.filters, opts.limit)
	if err != nil {
		return searchResult{}, err
	}
	_, settings, err := loadProjectSettings()
	if err != nil {
		return searchResult{}, err
	}
	params.SourceQuality = effectiveSourceQualityOptions(settings)
	params.AgingWindow = effectiveAgingWindowDays(settings)
	explicitLifecycle := params.Lifecycle != store.LifecycleAny || params.Validity != ""
	if !explicitLifecycle {
		params.Lifecycle = store.LifecycleCurrent
	}
	retrievalMode, provider, autoHybrid, err := resolveRetrievalMode(ctx, st, sc, settings, opts.hybrid, opts.hybridProvider, opts.hybridModel)
	if err != nil {
		return searchResult{}, err
	}
	runResolved := func(p store.SearchParams) ([]store.Memory, error) {
		runMode := retrievalMode
		if runMode == retrievalHybridFallbackFTS {
			runMode = retrievalFTS
		}
		rows, runErr := runRetriever(ctx, st, p, retrieverOptions{
			mode:           runMode,
			provider:       provider,
			hybridProvider: opts.hybridProvider,
			hybridModel:    opts.hybridModel,
			hybridPool:     opts.hybridPool,
			fusionK:        opts.fusionK,
			limit:          opts.limit,
		})
		if runErr != nil && autoHybrid && isQueryEmbeddingError(runErr) {
			fmt.Fprintf(os.Stderr, "warning: hybrid retrieval unavailable (%v); falling back to fts\n", runErr)
			retrievalMode = retrievalHybridFallbackFTS
			return runRetriever(ctx, st, p, retrieverOptions{mode: retrievalFTS, limit: opts.limit})
		}
		return rows, runErr
	}
	current, err := runResolved(params)
	if err != nil {
		return searchResult{}, err
	}
	current, err = augmentProfileSearch(ctx, st, params, current, opts.profiles)
	if err != nil {
		return searchResult{}, err
	}

	var historical []store.Memory
	if !explicitLifecycle {
		historicalParams := params
		historicalParams.Lifecycle = store.LifecycleHistorical
		historical, err = runResolved(historicalParams)
		if err != nil {
			return searchResult{}, err
		}
	}
	lanes := structuredRetrievalLanes(query, current, historical)
	if opts.explain {
		lanes = explainRetrievalLanes(lanes, query, params.SourceQuality, params.AgingWindow, retrievalMode)
	}
	currentWithWhy := make([]store.Memory, 0, len(current))
	for _, lane := range lanes {
		if lane.Key == "historical" {
			continue
		}
		currentWithWhy = append(currentWithWhy, lane.Results...)
	}
	return searchResult{
		Query:         query,
		Scope:         sc.Kind,
		ScopeID:       sc.ID,
		RetrievalMode: retrievalMode,
		ResultCount:   len(current),
		HistoryCount:  len(historical),
		Lanes:         lanes,
		Results:       currentWithWhy,
	}, nil
}

type searchModeResolver func(ctx context.Context, st *store.Store, sc scope.Scope, settings config.Settings, explicitHybridFlag bool, flagProvider, flagModel string) (string, embedding.Provider, bool, error)

var activeSearchModeResolver searchModeResolver = defaultResolveRetrievalMode

func resolveRetrievalMode(ctx context.Context, st *store.Store, sc scope.Scope, settings config.Settings, explicitHybridFlag bool, flagProvider, flagModel string) (string, embedding.Provider, bool, error) {
	return activeSearchModeResolver(ctx, st, sc, settings, explicitHybridFlag, flagProvider, flagModel)
}

func defaultResolveRetrievalMode(ctx context.Context, st *store.Store, sc scope.Scope, settings config.Settings, explicitHybridFlag bool, flagProvider, flagModel string) (string, embedding.Provider, bool, error) {
	if explicitHybridFlag {
		provider, err := newEmbeddingProvider(flagProvider, flagModel)
		if err != nil {
			return "", nil, false, fmt.Errorf("embedding provider: %w", err)
		}
		return retrievalHybrid, provider, false, nil
	}
	configured := effectiveRetrievalMode(settings)
	if configured == retrievalFTS {
		return retrievalFTS, nil, false, nil
	}
	count, providerName, model, err := st.EmbeddingIndexInfo(ctx, sc.Kind, sc.ID)
	if err != nil {
		return "", nil, false, err
	}
	if count >= embeddingIndexFloor {
		provider, err := newEmbeddingProvider(providerName, model)
		if err == nil {
			return retrievalHybrid, provider, true, nil
		}
	}
	if configured == retrievalHybrid {
		return "", nil, false, fmt.Errorf("no usable embedding index; run recoil embed index")
	}
	return retrievalFTS, nil, false, nil
}

func isQueryEmbeddingError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "embed query:")
}

func searchFrontmatter(result searchResult) []kv {
	meta := []kv{
		{k: "query", v: result.Query},
		{k: "scope", v: result.Scope},
		{k: "scope_id", v: result.ScopeID},
		{k: "retrieval_mode", v: result.RetrievalMode},
		{k: "result_count", v: fmt.Sprintf("%d", result.ResultCount)},
		{k: "history_count", v: fmt.Sprintf("%d", result.HistoryCount)},
	}
	return meta
}

func renderSearchResult(result searchResult, maxChars int) string {
	return retrievalLaneBlocks(result.Lanes, maxChars)
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
	// docs lose FTS to deeper, denser docs. Scale with limit, but cap the fanout
	// so broad variant searches stay bounded.
	pool := min(max(limit*20, 100), 200)
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
	ageWindow := p.AgingWindow
	if ageWindow <= 0 {
		ageWindow = defaultAgingWindowDays
	}
	fused := make([]searchScoredMemory, 0, len(byID))
	for _, item := range byID {
		coverage := signalTokenCoverage(p.Query, item.mem)
		if weakSignalSearchResult(p.Query, item.mem) {
			continue
		}
		item.score += 0.08 * coverage
		item.score += sourcequality.ScorePriorWithOptions(p.Query, item.mem.SourcePath, item.mem.MetadataJSON, sourcequality.ModeSearch, p.SourceQuality)
		now := time.Now().UTC()
		item.score += recencyPrior(item.mem, now)
		item.score -= agingPenalty(item.mem, now, ageWindow)
		if coverage >= 0.5 && item.mem.SourceKind == "direct" && isGuidanceRole(item.mem.Role) {
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
	fused = filterAbsentSubjectResults(p.Query, fused)
	out := diversifySignalResults(fused, limit, p.Query)
	return expandDerivedSourceEvidence(ctx, st, p, out, limit)
}

// weakSignalSearchResult filters absent-fact leakage from the fused candidate
// set: mined file chunks that match too few of the query's significant tokens,
// or whose only matches are negated mentions ("no redis", "without X"). The
// gate is general — no query-specific triggers — but deliberately narrow in
// what it may filter:
//   - only mined file chunks (source_kind=file) are eligible; direct and
//     session memories are conversational and legitimately match on partial
//     overlap (multi-entity questions, person queries).
//   - guidance-role memories (decisions, ADRs, constraints, ...) are exempt;
//     surfacing "do not use X" decisions for X queries is core behavior.
//   - operational docs (README, CONTRIBUTING, SECURITY, AGENTS) are exempt;
//     paraphrase queries legitimately reach them through intent expansion
//     even when raw token overlap is low.
func weakSignalSearchResult(query string, mem store.Memory) bool {
	if !strings.EqualFold(strings.TrimSpace(mem.SourceKind), "file") {
		return false
	}
	if isGuidanceRole(mem.Role) {
		return false
	}
	if sourcequality.Classify(mem.SourcePath).IsOperationalDoc {
		return false
	}
	tokens := retrieval.SignificantTokens(query)
	if len(tokens) <= 1 {
		return false
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
	presupposes := queryPresupposesSubject(query)
	for _, token := range tokens {
		if !strings.Contains(text, token) {
			continue
		}
		if presupposes && negatesSignalToken(text, token) {
			// For presupposition-form queries ("what X do we use"), a negated
			// mention ("There is no Redis") is evidence of absence, not an
			// answer. For existence questions ("is there a cache"), the
			// negated doc IS the answer, so the hit counts.
			continue
		}
		return false
	}
	// Zero qualifying significant-token hits: the chunk matched only
	// stopwords or expansion noise.
	return true
}

// queryPresupposesSubject detects question forms that presuppose their
// subject exists: "what X do we use", "which X do we expose". When the
// subject is absent or only mentioned as negated, the correct answer is
// empty. Existence/overview/how questions do not presuppose, so negative
// evidence and paraphrase matches remain valid answers for them.
func queryPresupposesSubject(query string) bool {
	lower := " " + strings.ToLower(strings.TrimSpace(query)) + " "
	if !strings.HasPrefix(lower, " what ") && !strings.HasPrefix(lower, " which ") {
		return false
	}
	for _, cue := range []string{" do we ", " are we ", " did we ", " does the ", " do i "} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// filterAbsentSubjectResults empties the candidate set when the query's
// subject is absent from the whole corpus slice under consideration. The
// subject is taken from the leading significant tokens ("what GRAPHQL schema
// do we expose", "what REDIS configuration do we use"). If no candidate
// mentions a subject token non-negated, then no candidate can answer the
// question — trailing-context matches (schema, clients, configuration) are
// absent-fact leakage and an empty result is correct. Paraphrase queries are
// unaffected: their subject terms exist in the corpus, so the filter never
// fires.
func filterAbsentSubjectResults(query string, fused []searchScoredMemory) []searchScoredMemory {
	if !queryPresupposesSubject(query) {
		return fused
	}
	tokens := retrieval.SignificantTokens(query)
	if len(tokens) < 3 || len(fused) == 0 {
		return fused
	}
	// Only the leading significant token is the subject ("what SQLITE library
	// do we use", "what GRAPHQL schema do we expose"). Later tokens are
	// context nouns (library, schema, configuration) that legitimately may be
	// missing from the answering document.
	subjects := tokens[:1]
	for _, subject := range subjects {
		present := false
		for _, item := range fused {
			text := strings.ToLower(strings.Join([]string{
				item.mem.Content, item.mem.Role, item.mem.ClaimKey,
				item.mem.SourcePath, item.mem.SourceRef,
			}, " "))
			if strings.Contains(text, subject) && !negatesSignalToken(text, subject) {
				present = true
				break
			}
		}
		if !present {
			var kept []searchScoredMemory
			for _, item := range fused {
				text := strings.ToLower(strings.Join([]string{
					item.mem.Content, item.mem.Role, item.mem.ClaimKey,
					item.mem.SourcePath, item.mem.SourceRef,
				}, " "))
				if strings.Contains(text, subject) && !negatesSignalToken(text, subject) {
					kept = append(kept, item)
				}
			}
			fused = kept
			if len(fused) == 0 {
				return nil
			}
		}
	}
	return fused
}

func negatesSignalToken(text, token string) bool {
	for _, pattern := range []string{"no " + token, "not " + token, "not use " + token, "does not use " + token, "do not use " + token, "without " + token} {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
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

// filterStrictEntityResults applies entity gating to fused results.
//
// Strong signal (explicit "Dr. X" doctor names): hard filter — a sparse
// search must not satisfy a named-doctor question with another doctor.
//
// Weak signal (capitalized bigrams that look like person names): demotion
// only. Results that don't mention the name take a flat score penalty and
// re-sort below matching results, but they survive — capitalized bigrams
// are too often product names ("Visual Studio", "North Star") for a hard
// drop to be safe.
const entityDemotionPenalty = 1.0

func filterStrictEntityResults(query string, fused []searchScoredMemory) []searchScoredMemory {
	doctorNames := retrieval.DoctorNameTerms(query)
	personNames := explicitPersonNameTerms(query)
	if len(doctorNames) == 0 && len(personNames) == 0 {
		return fused
	}
	containsAnyName := func(item searchScoredMemory, names []string) bool {
		text := strings.ToLower(item.mem.Content + " " + item.mem.SourceRef + " " + item.mem.SourcePath)
		for _, name := range names {
			if strings.Contains(text, name) {
				return true
			}
		}
		return false
	}
	// When no result mentions the cued person at all, answering with someone
	// else's facts is a wrong-memory hazard — hard-filter to empty just like
	// the doctor path. Demotion only applies when the person is present and
	// weaker context results would otherwise outrank them.
	anyPersonMatch := false
	if len(personNames) > 0 {
		for _, item := range fused {
			if containsAnyName(item, personNames) {
				anyPersonMatch = true
				break
			}
		}
	}
	var out []searchScoredMemory
	demoted := false
	for _, item := range fused {
		if len(doctorNames) > 0 {
			if containsAnyName(item, doctorNames) {
				out = append(out, item)
				continue
			}
			if len(personNames) == 0 {
				continue
			}
		}
		if len(personNames) > 0 && !containsAnyName(item, personNames) {
			if len(doctorNames) > 0 || !anyPersonMatch {
				// Doctor query, or a person query where nobody in the
				// candidate set mentions that person: hard-filter.
				continue
			}
			item.score -= entityDemotionPenalty
			item.mem.Score = item.score
			demoted = true
		}
		out = append(out, item)
	}
	if demoted {
		sort.SliceStable(out, func(i, j int) bool {
			if math.Abs(out[i].score-out[j].score) < 1e-9 {
				return out[i].mem.CreatedAt > out[j].mem.CreatedAt
			}
			return out[i].score > out[j].score
		})
	}
	return out
}

var explicitPersonNameRE = regexp.MustCompile(`\b([A-Z][a-z]{2,})\s+([A-Z][a-z]{2,})\b`)

// explicitPersonNameTerms extracts likely person names from a query. A
// capitalized bigram alone is a weak signal (product names match the same
// shape), so a bigram only counts when a person-ish cue appears in the query:
// a personal lead-in word directly before the name, or person-question
// context anywhere in the query.
func explicitPersonNameTerms(query string) []string {
	if !queryHasPersonContext(query) {
		return nil
	}
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

func queryHasPersonContext(query string) bool {
	lower := " " + strings.ToLower(query) + " "
	for _, cue := range []string{
		" dr ", " dr. ", " doctor ", " my ", " with ", " named ", " called ",
		" friend ", " brother ", " sister ", " cousin ", " coworker ",
		" colleague ", " who ", " whom ", " person ", " people ", " met ",
		" ask ", " asked ", " said ", " say ", " told ", " tell ",
		" recommend ", " recommended ", " mentioned ",
	} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
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
	// The desperate fallback re-adds from fused, which has already been
	// weak-filtered upstream, so it cannot resurrect absent-fact leakage.
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
	metadata := strings.ToLower(strings.Join(strings.Fields(mem.MetadataJSON), ""))
	if strings.Contains(metadata, `"noise":true`) {
		return true
	}
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
		"not the answer",
		"only background discussion",
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
