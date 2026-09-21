package cmd

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/sourcequality"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type wakeOptions struct {
	scope     scopeOptions
	filters   memoryFilterOptions
	limit     int
	maxChars  int
	minimal   bool
	decisions bool
	explain   bool
}

type wakeResult struct {
	Query            string                 `json:"query,omitempty"`
	Scope            string                 `json:"scope"`
	ScopeID          string                 `json:"scope_id"`
	SelectedCount    int                    `json:"selected_count"`
	Refresh          sourceRefreshSummary   `json:"refresh"`
	ChannelRefresh   []channelRefreshResult `json:"channel_refresh,omitempty"`
	ChannelFreshness channelFreshnessResult `json:"channel_freshness"`
	Layers           []wakeLayerResult      `json:"layers"`
	Results          []store.Memory         `json:"results"`
	DecisionTrail    []decisionTrailItem    `json:"decision_trail,omitempty"`
}

type wakeLayerResult struct {
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Results []store.Memory `json:"results"`
}

type wakeLayer struct {
	Key      string
	Title    string
	Memories []store.Memory
}

type wakeRender struct {
	Body       string
	ShownCount int
	Truncated  bool
}

type wakeExecution struct {
	Result wakeResult
	Layers []wakeLayer
}

func newWakeCommand() *cobra.Command {
	var wakeOpts wakeOptions
	c := &cobra.Command{
		Use:   "wake [query]",
		Short: "Print bounded starter memory context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			sc, err := resolveWakeScope(cmd, wakeOpts.scope)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			exec, err := runWake(ctx, st, sc, query, wakeOpts)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "wake_result", exec.Result)
			}
			if wakeOpts.minimal {
				for _, mem := range exec.Result.Results {
					writeMinimalMemory(w, mem, mem.Score > 0)
				}
				return nil
			}
			body, rendered := renderWakeExecution(ctx, st, exec, wakeOpts.maxChars, true)
			return frontmatter(w, wakeFrontmatter(exec.Result, rendered, wakeOpts.maxChars), body)
		},
	}
	addScopeFlags(c, &wakeOpts.scope)
	addMemoryFilterFlags(c, &wakeOpts.filters)
	c.Flags().IntVar(&wakeOpts.limit, "limit", 8, "maximum number of memories to include")
	c.Flags().IntVar(&wakeOpts.maxChars, "max-chars", 1600, "maximum characters of memory content to print")
	c.Flags().BoolVar(&wakeOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().BoolVar(&wakeOpts.decisions, "include-decisions", false, "include a claim-keyed decision trail")
	c.Flags().BoolVar(&wakeOpts.explain, "explain", false, "include per-result score component diagnostics")
	return c
}

func runWake(ctx context.Context, st *store.Store, sc scope.Scope, query string, opts wakeOptions) (wakeExecution, error) {
	_, settings, err := loadProjectSettings()
	if err != nil {
		return wakeExecution{}, err
	}
	qualityOpts := effectiveSourceQualityOptions(settings)
	ageWindow := effectiveAgingWindowDays(settings)
	refresh, err := refreshTrackedProjectSources(ctx, st, sc)
	if err != nil {
		return wakeExecution{}, err
	}
	channelFreshness := ensureFreshContext(ctx, st, "wake")
	channelRefresh := channelFreshness.Refresh
	fetchLimit := wakeFetchLimit(opts.limit)
	var queryResults []store.Memory
	if query != "" {
		params, err := searchParams(query, sc, opts.filters, fetchLimit)
		if err != nil {
			return wakeExecution{}, err
		}
		if params.Validity == "" && params.Lifecycle == store.LifecycleAny {
			params.Lifecycle = store.LifecycleCurrent
		}
		params.SourceQuality = qualityOpts
		// Wake remains FTS-only: its recency/layering surface gains little from embeddings.
		found, err := runRetriever(ctx, st, params, retrieverOptions{
			mode:  retrievalFTS,
			limit: fetchLimit,
		})
		if err != nil {
			return wakeExecution{}, err
		}
		queryResults = found
	}

	recent, err := wakeRecentMemories(ctx, st, sc, opts.filters, fetchLimit, opts.limit, qualityOpts)
	if err != nil {
		return wakeExecution{}, err
	}
	layers := buildWakeLayers(query, queryResults, recent, opts.limit, qualityOpts, ageWindow)
	if opts.explain {
		layers = explainWakeLayers(layers, query, qualityOpts, ageWindow)
	}
	results := flattenWakeLayers(layers)
	var trail []decisionTrailItem
	if opts.decisions {
		trail, err = decisionTrail(ctx, st, sc, opts.limit)
		if err != nil {
			return wakeExecution{}, err
		}
	}
	result := wakeResult{
		Query:            query,
		Scope:            sc.Kind,
		ScopeID:          sc.ID,
		SelectedCount:    len(results),
		Refresh:          refresh,
		ChannelRefresh:   channelRefresh,
		ChannelFreshness: channelFreshness,
		Layers:           wakeLayerResults(layers),
		Results:          results,
		DecisionTrail:    trail,
	}
	return wakeExecution{Result: result, Layers: layers}, nil
}

func renderWakeExecution(ctx context.Context, st *store.Store, exec wakeExecution, maxChars int, includePresence bool) (string, wakeRender) {
	rendered := layeredMemoryBlocks(exec.Layers, maxChars, true)
	body := rendered.Body
	if includePresence {
		if presence := swarmPresenceLine(ctx, st, exec.Result.ChannelFreshness); presence != "" {
			body = presence + "\n\n" + body
		}
	}
	if len(exec.Result.DecisionTrail) > 0 {
		body = combineWakeDecisionTrail(renderDecisionTrail(exec.Result.DecisionTrail), body)
	}
	return body, rendered
}

func wakeFrontmatter(result wakeResult, rendered wakeRender, maxChars int) []kv {
	meta := []kv{
		{k: "query", v: result.Query},
		{k: "scope", v: result.Scope},
		{k: "scope_id", v: result.ScopeID},
		{k: "result_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
		{k: "selected_count", v: fmt.Sprintf("%d", result.SelectedCount)},
		{k: "shown_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
		{k: "refreshed_sources", v: fmt.Sprintf("%d", result.Refresh.RefreshedSources)},
		{k: "staled_memories", v: fmt.Sprintf("%d", result.Refresh.StaledMemories)},
		{k: "channel_imported", v: fmt.Sprintf("%d", channelFreshnessImported(result.ChannelFreshness))},
		{k: "channel_errors", v: fmt.Sprintf("%d", channelFreshnessErrors(result.ChannelFreshness))},
		{k: "truncated", v: fmt.Sprintf("%t", rendered.Truncated)},
		{k: "max_chars", v: fmt.Sprintf("%d", maxChars)},
	}
	meta = append(meta, channelFriendlyFrontmatter(result.ChannelFreshness)...)
	meta = append(meta, channelOutboxFrontmatter("channel_", result.ChannelFreshness.Outbox)...)
	return meta
}

func wakeRecentMemories(ctx context.Context, st *store.Store, sc scope.Scope, filters memoryFilterOptions, fetchLimit, displayLimit int, qualityOpts ...sourcequality.Options) ([]store.Memory, error) {
	if hasExplicitWakeFilters(filters) {
		params, err := listParams(sc, filters, fetchLimit, false)
		if err != nil {
			return nil, err
		}
		if params.Validity == "" && params.Lifecycle == store.LifecycleAny {
			params.Lifecycle = store.LifecycleCurrent
		}
		return st.List(ctx, params)
	}

	quota := displayLimit
	if quota <= 0 {
		quota = 8
	}
	seen := map[string]bool{}
	var out []store.Memory
	add := func(memories []store.Memory) {
		for _, mem := range memories {
			if len(out) >= fetchLimit {
				return
			}
			if seen[mem.ID] {
				continue
			}
			seen[mem.ID] = true
			out = append(out, mem)
		}
	}
	list := func(mod func(*store.ListParams), limit int) error {
		params, err := listParams(sc, filters, limit, false)
		if err != nil {
			return err
		}
		params.Lifecycle = store.LifecycleCurrent
		mod(&params)
		memories, err := st.List(ctx, params)
		if err != nil {
			return err
		}
		add(memories)
		return nil
	}

	if err := list(func(p *store.ListParams) { p.SourcePath = "HANDOFF.md" }, 2); err != nil {
		return nil, err
	}
	if err := list(func(p *store.ListParams) {
		p.Role = "handoff"
		p.SourceKind = "direct"
	}, 2); err != nil {
		return nil, err
	}
	for _, sourcePath := range sourcequality.OperationalSourcePaths() {
		if err := list(func(p *store.ListParams) {
			p.SourceKind = "file"
			p.SourcePath = sourcePath
		}, 2); err != nil {
			return nil, err
		}
	}
	l1Quota := quota / 2
	if l1Quota < 3 {
		l1Quota = 3
	}
	for _, role := range []string{"adr", "decision", "constraint", "preference", "rule"} {
		if err := list(func(p *store.ListParams) { p.Role = role }, l1Quota); err != nil {
			return nil, err
		}
	}
	if err := list(func(p *store.ListParams) {}, fetchLimit); err != nil {
		return nil, err
	}
	return out, nil
}

func hasExplicitWakeFilters(filters memoryFilterOptions) bool {
	return strings.TrimSpace(filters.agent) != "" ||
		strings.TrimSpace(filters.sourceKind) != "" ||
		strings.TrimSpace(filters.source) != "" ||
		strings.TrimSpace(filters.role) != "" ||
		strings.TrimSpace(filters.claimKey) != "" ||
		strings.TrimSpace(filters.validity) != "" ||
		strings.TrimSpace(filters.since) != "" ||
		strings.TrimSpace(filters.before) != "" ||
		filters.historical
}

func wakeFetchLimit(limit int) int {
	if limit <= 0 {
		limit = 8
	}
	fetchLimit := limit * 4
	if fetchLimit < 20 {
		return 20
	}
	if fetchLimit > 1000 {
		return 1000
	}
	return fetchLimit
}

func channelRefreshImported(results []channelRefreshResult) int {
	total := 0
	for _, result := range results {
		total += result.Imported
	}
	return total
}

func channelRefreshErrors(results []channelRefreshResult) int {
	total := 0
	for _, result := range results {
		if result.Error != "" {
			total++
		}
	}
	return total
}

func buildWakeLayers(query string, queryResults, recent []store.Memory, limit int, quality sourcequality.Options, ageWindow float64) []wakeLayer {
	if limit <= 0 {
		limit = 8
	}
	if ageWindow <= 0 {
		ageWindow = defaultAgingWindowDays
	}
	queryResults = rankWakeCandidates(queryResults, query, quality)
	recent = rankWakeCandidates(recent, wakePolicyQuery(query), quality)
	layers := []wakeLayer{
		{Key: "current_decisions", Title: "Current Decisions"},
		{Key: "remote_artifacts", Title: "Peer Memory"},
		{Key: "project_docs", Title: "Project Docs"},
		{Key: "recent_evidence", Title: "Recent Evidence"},
	}
	seen := make(map[string]bool)
	sessionEvidenceByLayer := map[int]int{}
	total := 0
	newestDirectHandoff := newestDirectHandoffID(append(append([]store.Memory(nil), queryResults...), recent...))
	layerCaps := []int{max(2, limit/3), limit, (limit + 1) / 2, 2}
	type wakeCandidate struct {
		memory    store.Memory
		fromQuery bool
	}
	candidates := make([]wakeCandidate, 0, len(queryResults)+len(recent))
	for _, mem := range queryResults {
		candidates = append(candidates, wakeCandidate{memory: mem, fromQuery: true})
	}
	for _, mem := range recent {
		candidates = append(candidates, wakeCandidate{memory: mem})
	}
	add := func(mem store.Memory, fromQuery bool, enforceLayerCap bool) (bool, bool) {
		if total >= limit || seen[mem.ID] {
			return false, false
		}
		if !wakeEligibleMemory(mem, query, newestDirectHandoff) {
			return false, false
		}
		layerIndex := classifyWakeMemory(mem, query, fromQuery)
		if isSessionEvidence(mem) {
			if sessionEvidenceByLayer[layerIndex] >= 2 {
				return false, false
			}
		}
		if enforceLayerCap && layerIndex >= 0 && layerIndex < len(layerCaps) && len(layers[layerIndex].Memories) >= layerCaps[layerIndex] {
			return false, true
		}
		mem.Why = whyMemorySurfaced(mem, query, fromQuery, false)
		if suffix := agingWhySuffix(mem, time.Now().UTC(), ageWindow); suffix != "" && !strings.Contains(mem.Why, "aging —") {
			mem.Why += suffix
		}
		if isSessionEvidence(mem) {
			sessionEvidenceByLayer[layerIndex]++
		}
		layers[layerIndex].Memories = append(layers[layerIndex].Memories, mem)
		seen[mem.ID] = true
		total++
		return true, false
	}
	var skipped []wakeCandidate
	for _, candidate := range candidates {
		if total >= limit {
			break
		}
		if _, quotaSkipped := add(candidate.memory, candidate.fromQuery, true); quotaSkipped {
			skipped = append(skipped, candidate)
		}
	}
	for _, candidate := range skipped {
		if total >= limit {
			break
		}
		if classifyWakeMemory(candidate.memory, query, candidate.fromQuery) == 2 && total > len(layers[2].Memories) {
			continue
		}
		add(candidate.memory, candidate.fromQuery, false)
	}
	return layers
}

func wakeEligibleMemory(mem store.Memory, query, newestDirectHandoff string) bool {
	if isHistoricalMemory(mem) {
		return false
	}
	if broken, _ := deterministicPredicateBroken(mem); broken {
		return false
	}
	if isDirectHandoff(mem) && newestDirectHandoff != "" && mem.ID != newestDirectHandoff {
		return false
	}
	if isNegativeEvidence(mem) && !queryAsksForNegativeEvidence(query) {
		return false
	}
	return true
}

func rankWakeCandidates(memories []store.Memory, query string, quality sourcequality.Options) []store.Memory {
	if len(memories) <= 1 {
		return append([]store.Memory(nil), memories...)
	}
	ranked := append([]store.Memory(nil), memories...)
	now := time.Now().UTC()
	for i := range ranked {
		ranked[i].Score += sourcequality.ScorePriorWithOptions(query, ranked[i].SourcePath, ranked[i].MetadataJSON, sourcequality.ModeWake, quality)
		ranked[i].Score += recencyPrior(ranked[i], now)
		ranked[i].Score -= agingPenalty(ranked[i], now, defaultAgingWindowDays)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if math.Abs(ranked[i].Score-ranked[j].Score) < 1e-9 {
			return ranked[i].CreatedAt > ranked[j].CreatedAt
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}

func wakePolicyQuery(query string) string {
	if strings.TrimSpace(query) != "" {
		return query
	}
	return "agent onboarding before editing contribute security tests work in this repo"
}

func isDirectHandoff(mem store.Memory) bool {
	return strings.EqualFold(strings.TrimSpace(mem.Role), "handoff") && strings.EqualFold(strings.TrimSpace(mem.SourceKind), "direct")
}

func newestDirectHandoffID(memories []store.Memory) string {
	newestID := ""
	newestCreated := ""
	for _, mem := range memories {
		if !isDirectHandoff(mem) || isHistoricalMemory(mem) {
			continue
		}
		if broken, _ := deterministicPredicateBroken(mem); broken {
			continue
		}
		if newestID == "" || mem.CreatedAt > newestCreated || (mem.CreatedAt == newestCreated && mem.ID > newestID) {
			newestID = mem.ID
			newestCreated = mem.CreatedAt
		}
	}
	return newestID
}

func classifyWakeMemory(mem store.Memory, _ string, _ bool) int {
	role := strings.ToLower(mem.Role)
	sourcePath := strings.ToLower(mem.SourcePath)
	content := strings.ToLower(mem.Content)
	if strings.EqualFold(mem.SourceKind, "remote_artifact") {
		return 1
	}
	if strings.EqualFold(mem.SourceKind, "file") {
		return 2
	}
	if isSessionEvidence(mem) {
		return 3
	}
	if strings.Contains(sourcePath, "handoff") ||
		strings.Contains(content, "handoff") ||
		strings.Contains(content, "next active task") ||
		strings.Contains(content, "next build step") ||
		strings.Contains(content, "current task") {
		return 3
	}
	switch role {
	case "decision", "adr", "constraint", "preference", "rule":
		return 0
	}
	if strings.Contains(content, "do not ") ||
		strings.Contains(content, "must ") ||
		strings.Contains(content, "non-goal") ||
		strings.Contains(content, "settled decision") {
		return 0
	}
	return 3
}

func isSessionEvidence(mem store.Memory) bool {
	return strings.EqualFold(mem.SourceKind, "session_evidence")
}

func wakeLayerResults(layers []wakeLayer) []wakeLayerResult {
	results := make([]wakeLayerResult, 0, len(layers))
	for _, layer := range layers {
		if len(layer.Memories) == 0 {
			continue
		}
		results = append(results, wakeLayerResult{
			Key:     layer.Key,
			Title:   layer.Title,
			Results: layer.Memories,
		})
	}
	return results
}

func flattenWakeLayers(layers []wakeLayer) []store.Memory {
	var results []store.Memory
	for _, layer := range layers {
		results = append(results, layer.Memories...)
	}
	return results
}

func layeredMemoryBlocks(layers []wakeLayer, maxChars int, includeScore bool) wakeRender {
	var b strings.Builder
	remaining := maxChars
	selectedCount := len(flattenWakeLayers(layers))
	perMemCap := 0
	if maxChars > 0 {
		perMemCap = max(280, maxChars/max(1, selectedCount))
	}
	render := wakeRender{}
	for _, layer := range layers {
		if len(layer.Memories) == 0 {
			continue
		}
		header := fmt.Sprintf("## %s\n", layer.Title)
		if !appendBounded(&b, header, &remaining, maxChars) {
			render.Truncated = true
			render.Body = strings.TrimRight(b.String(), "\n")
			return render
		}
		for _, mem := range layer.Memories {
			shown, complete, exhausted := appendMemoryBlockBounded(&b, mem, &remaining, maxChars, includeScore, perMemCap)
			if shown {
				render.ShownCount++
			}
			if !complete {
				render.Truncated = true
				if exhausted {
					render.Body = strings.TrimRight(b.String(), "\n")
					return render
				}
			}
		}
	}
	if selectedCount == 0 {
		render.Body = "No memories found."
		return render
	}
	render.Body = strings.TrimRight(b.String(), "\n")
	return render
}

func appendMemoryBlockBounded(b *strings.Builder, mem store.Memory, remaining *int, maxChars int, includeScore bool, perMemCap int) (bool, bool, bool) {
	var meta strings.Builder
	fmt.Fprintf(&meta, "### %s\n", mem.ID)
	if includeScore && mem.Score > 0 {
		fmt.Fprintf(&meta, "score: %.4f\n", mem.Score)
	}
	fmt.Fprintf(&meta, "created: %s\n", mem.CreatedAt)
	if mem.Role != "" {
		fmt.Fprintf(&meta, "role: %s\n", mem.Role)
	}
	if mem.SourceKind != "" {
		fmt.Fprintf(&meta, "source_kind: %s\n", mem.SourceKind)
	}
	for _, line := range sessionEvidenceProvenanceLines(mem) {
		fmt.Fprintf(&meta, "%s\n", line)
	}
	if mem.Validity != "" {
		fmt.Fprintf(&meta, "validity: %s\n", mem.Validity)
	}
	if mem.ClaimKey != "" {
		fmt.Fprintf(&meta, "claim_key: %s\n", mem.ClaimKey)
	}
	if mem.Supersedes != "" {
		fmt.Fprintf(&meta, "supersedes: %s\n", mem.Supersedes)
	}
	if mem.SupersededBy != "" {
		fmt.Fprintf(&meta, "superseded_by: %s\n", mem.SupersededBy)
	}
	if mem.SourceAgent != "" {
		fmt.Fprintf(&meta, "source_agent: %s\n", mem.SourceAgent)
	}
	if mem.SourcePath != "" {
		fmt.Fprintf(&meta, "source_path: %s\n", mem.SourcePath)
	}
	if sourceRef := sessionEvidenceSourceRef(mem); sourceRef != "" {
		fmt.Fprintf(&meta, "source_ref: %s\n", sourceRef)
	}
	if mem.Why != "" {
		fmt.Fprintf(&meta, "why: %s\n", mem.Why)
	}
	renderExplainComponents(&meta, mem.Explain)
	meta.WriteString("\n")

	if maxChars <= 0 {
		b.WriteString(meta.String())
		b.WriteString(mem.Content)
		b.WriteString("\n\n")
		return true, true, false
	}
	overhead := meta.Len() + 2
	if *remaining <= overhead {
		return false, false, true
	}
	contentLimit := *remaining - overhead
	if perMemCap > 0 && perMemCap < contentLimit {
		contentLimit = perMemCap
	}
	content := truncateText(mem.Content, contentLimit)
	block := meta.String() + content + "\n\n"
	appendedComplete := appendBounded(b, block, remaining, maxChars)
	complete := appendedComplete
	if len(mem.Content) > contentLimit {
		complete = false
	}
	return true, complete, !appendedComplete
}

func appendBounded(b *strings.Builder, s string, remaining *int, maxChars int) bool {
	if maxChars <= 0 {
		b.WriteString(s)
		return true
	}
	if *remaining <= 0 {
		return false
	}
	if len(s) > *remaining {
		prefix := safeBytePrefix(s, *remaining)
		b.WriteString(prefix)
		*remaining -= len(prefix)
		return false
	}
	b.WriteString(s)
	*remaining -= len(s)
	return true
}
