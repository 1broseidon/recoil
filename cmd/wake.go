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
}

type wakeResult struct {
	Query          string                 `json:"query,omitempty"`
	Scope          string                 `json:"scope"`
	ScopeID        string                 `json:"scope_id"`
	SelectedCount  int                    `json:"selected_count"`
	Refresh        sourceRefreshSummary   `json:"refresh"`
	ChannelRefresh []channelRefreshResult `json:"channel_refresh,omitempty"`
	Layers         []wakeLayerResult      `json:"layers"`
	Results        []store.Memory         `json:"results"`
	DecisionTrail  []decisionTrailItem    `json:"decision_trail,omitempty"`
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

			_, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			qualityOpts := effectiveSourceQualityOptions(settings)
			ctx := context.Background()
			refresh, err := refreshTrackedProjectSources(ctx, st, sc)
			if err != nil {
				return err
			}
			channelRefresh := wakeChannelRefresh(ctx, st)
			fetchLimit := wakeFetchLimit(wakeOpts.limit)
			var queryResults []store.Memory
			if query != "" {
				params, err := searchParams(query, sc, wakeOpts.filters, fetchLimit)
				if err != nil {
					return err
				}
				if params.Validity == "" && params.Lifecycle == store.LifecycleAny {
					params.Lifecycle = store.LifecycleCurrent
				}
				params.SourceQuality = qualityOpts
				found, err := runRetriever(ctx, st, params, retrieverOptions{
					mode:  retrievalFTS,
					limit: fetchLimit,
				})
				if err != nil {
					return err
				}
				queryResults = found
			}

			recent, err := wakeRecentMemories(ctx, st, sc, wakeOpts.filters, fetchLimit, wakeOpts.limit, qualityOpts)
			if err != nil {
				return err
			}
			layers := buildWakeLayers(query, queryResults, recent, wakeOpts.limit, qualityOpts)
			results := flattenWakeLayers(layers)

			w := cmd.OutOrStdout()
			var trail []decisionTrailItem
			if wakeOpts.decisions {
				trail, err = decisionTrail(ctx, st, sc, wakeOpts.limit)
				if err != nil {
					return err
				}
			}
			result := wakeResult{
				Query:          query,
				Scope:          sc.Kind,
				ScopeID:        sc.ID,
				SelectedCount:  len(results),
				Refresh:        refresh,
				ChannelRefresh: channelRefresh,
				Layers:         wakeLayerResults(layers),
				Results:        results,
				DecisionTrail:  trail,
			}
			if opts.json {
				return writeJSON(w, "wake_result", result)
			}
			if wakeOpts.minimal {
				for _, mem := range results {
					writeMinimalMemory(w, mem, mem.Score > 0)
				}
				return nil
			}
			rendered := layeredMemoryBlocks(layers, wakeOpts.maxChars, true)
			body := rendered.Body
			if wakeOpts.decisions {
				body = combineWakeDecisionTrail(renderDecisionTrail(trail), body)
			}
			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
				{k: "selected_count", v: fmt.Sprintf("%d", len(results))},
				{k: "shown_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
				{k: "refreshed_sources", v: fmt.Sprintf("%d", refresh.RefreshedSources)},
				{k: "staled_memories", v: fmt.Sprintf("%d", refresh.StaledMemories)},
				{k: "channel_imported", v: fmt.Sprintf("%d", channelRefreshImported(channelRefresh))},
				{k: "channel_errors", v: fmt.Sprintf("%d", channelRefreshErrors(channelRefresh))},
				{k: "truncated", v: fmt.Sprintf("%t", rendered.Truncated)},
				{k: "max_chars", v: fmt.Sprintf("%d", wakeOpts.maxChars)},
			}, body)
		},
	}
	addScopeFlags(c, &wakeOpts.scope)
	addMemoryFilterFlags(c, &wakeOpts.filters)
	c.Flags().IntVar(&wakeOpts.limit, "limit", 8, "maximum number of memories to include")
	c.Flags().IntVar(&wakeOpts.maxChars, "max-chars", 1600, "maximum characters of memory content to print")
	c.Flags().BoolVar(&wakeOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().BoolVar(&wakeOpts.decisions, "include-decisions", false, "include a claim-keyed decision trail")
	return c
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

func wakeChannelRefresh(ctx context.Context, st *store.Store) []channelRefreshResult {
	channels, err := st.ChannelSubscriptions(ctx)
	if err != nil || len(channels) == 0 {
		return nil
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	identity, err := st.GetOrCreateChannelIdentity(refreshCtx)
	if err != nil {
		return []channelRefreshResult{{Error: err.Error()}}
	}
	return refreshChannels(refreshCtx, st, identity, channels)
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

func buildWakeLayers(query string, queryResults, recent []store.Memory, limit int, qualityOpts ...sourcequality.Options) []wakeLayer {
	if limit <= 0 {
		limit = 8
	}
	var quality sourcequality.Options
	if len(qualityOpts) > 0 {
		quality = qualityOpts[0]
	}
	queryResults = rankWakeCandidates(queryResults, query, quality)
	recent = rankWakeCandidates(recent, wakePolicyQuery(query), quality)
	layers := []wakeLayer{
		{Key: "current_decisions", Title: "Current Decisions"},
		{Key: "remote_artifacts", Title: "Remote Artifacts"},
		{Key: "project_docs", Title: "Project Docs"},
		{Key: "recent_evidence", Title: "Recent Evidence"},
	}
	seen := make(map[string]bool)
	sessionEvidenceByLayer := map[int]int{}
	total := 0
	add := func(mem store.Memory, fromQuery bool) {
		if total >= limit || seen[mem.ID] {
			return
		}
		if isHistoricalMemory(mem) {
			return
		}
		if isNegativeEvidence(mem) && !queryAsksForNegativeEvidence(query) {
			return
		}
		layerIndex := classifyWakeMemory(mem, query, fromQuery)
		mem.Why = whyMemorySurfaced(mem, query, fromQuery, false)
		if isSessionEvidence(mem) && !fromQuery {
			if layerIndex == 0 {
				layerIndex = 3
			}
			if layerIndex == 3 && sessionEvidenceByLayer[layerIndex] >= 2 {
				return
			}
			sessionEvidenceByLayer[layerIndex]++
		}
		layers[layerIndex].Memories = append(layers[layerIndex].Memories, mem)
		seen[mem.ID] = true
		total++
	}
	for _, mem := range queryResults {
		add(mem, true)
	}
	for _, mem := range recent {
		add(mem, false)
	}
	return layers
}

func rankWakeCandidates(memories []store.Memory, query string, quality sourcequality.Options) []store.Memory {
	if len(memories) <= 1 {
		return memories
	}
	ranked := append([]store.Memory(nil), memories...)
	for i := range ranked {
		ranked[i].Score += sourcequality.ScorePriorWithOptions(query, ranked[i].SourcePath, ranked[i].MetadataJSON, sourcequality.ModeWake, quality)
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

func classifyWakeMemory(mem store.Memory, query string, fromQuery bool) int {
	role := strings.ToLower(mem.Role)
	sourcePath := strings.ToLower(mem.SourcePath)
	content := strings.ToLower(mem.Content)
	if strings.EqualFold(mem.SourceKind, "remote_artifact") {
		return 1
	}
	if strings.EqualFold(mem.SourceKind, "file") {
		return 2
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
	if fromQuery && strings.TrimSpace(query) != "" {
		return 3
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
			shown, complete := appendMemoryBlockBounded(&b, mem, &remaining, maxChars, includeScore)
			if shown {
				render.ShownCount++
			}
			if !complete {
				render.Truncated = true
				render.Body = strings.TrimRight(b.String(), "\n")
				return render
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

func appendMemoryBlockBounded(b *strings.Builder, mem store.Memory, remaining *int, maxChars int, includeScore bool) (bool, bool) {
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
	if mem.SourceRef != "" {
		fmt.Fprintf(&meta, "source_ref: %s\n", mem.SourceRef)
	}
	if mem.Why != "" {
		fmt.Fprintf(&meta, "why: %s\n", mem.Why)
	}
	meta.WriteString("\n")

	if maxChars <= 0 {
		b.WriteString(meta.String())
		b.WriteString(mem.Content)
		b.WriteString("\n\n")
		return true, true
	}
	overhead := meta.Len() + 2
	if *remaining <= overhead {
		return false, false
	}
	contentLimit := *remaining - overhead
	content := truncateText(mem.Content, contentLimit)
	block := meta.String() + content + "\n\n"
	complete := appendBounded(b, block, remaining, maxChars)
	if len(mem.Content) > contentLimit {
		complete = false
	}
	return true, complete
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

func appendUnique(dst *[]store.Memory, seen map[string]bool, src []store.Memory, limit int) {
	for _, mem := range src {
		if seen[mem.ID] {
			continue
		}
		seen[mem.ID] = true
		*dst = append(*dst, mem)
		if limit > 0 && len(*dst) >= limit {
			return
		}
	}
}
