package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type wakeOptions struct {
	scope    scopeOptions
	filters  memoryFilterOptions
	limit    int
	maxChars int
	minimal  bool
}

type wakeResult struct {
	Query         string            `json:"query,omitempty"`
	Scope         string            `json:"scope"`
	ScopeID       string            `json:"scope_id"`
	SelectedCount int               `json:"selected_count"`
	Layers        []wakeLayerResult `json:"layers"`
	Results       []store.Memory    `json:"results"`
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

			ctx := context.Background()
			fetchLimit := wakeFetchLimit(wakeOpts.limit)
			var queryResults []store.Memory
			if query != "" {
				params, err := searchParams(query, sc, wakeOpts.filters, fetchLimit)
				if err != nil {
					return err
				}
				params.Lifecycle = store.LifecycleCurrent
				found, err := st.Search(ctx, params)
				if err != nil {
					return err
				}
				queryResults = found
			}

			params, err := listParams(sc, wakeOpts.filters, fetchLimit, false)
			if err != nil {
				return err
			}
			params.Lifecycle = store.LifecycleCurrent
			recent, err := st.List(ctx, params)
			if err != nil {
				return err
			}
			layers := buildWakeLayers(query, queryResults, recent, wakeOpts.limit)
			results := flattenWakeLayers(layers)

			w := cmd.OutOrStdout()
			result := wakeResult{
				Query:         query,
				Scope:         sc.Kind,
				ScopeID:       sc.ID,
				SelectedCount: len(results),
				Layers:        wakeLayerResults(layers),
				Results:       results,
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
			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
				{k: "selected_count", v: fmt.Sprintf("%d", len(results))},
				{k: "shown_count", v: fmt.Sprintf("%d", rendered.ShownCount)},
				{k: "truncated", v: fmt.Sprintf("%t", rendered.Truncated)},
				{k: "max_chars", v: fmt.Sprintf("%d", wakeOpts.maxChars)},
			}, rendered.Body)
		},
	}
	addScopeFlags(c, &wakeOpts.scope)
	addMemoryFilterFlags(c, &wakeOpts.filters)
	c.Flags().IntVar(&wakeOpts.limit, "limit", 8, "maximum number of memories to include")
	c.Flags().IntVar(&wakeOpts.maxChars, "max-chars", 1600, "maximum characters of memory content to print")
	c.Flags().BoolVar(&wakeOpts.minimal, "minimal", false, "print tab-separated rows")
	return c
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

func buildWakeLayers(query string, queryResults, recent []store.Memory, limit int) []wakeLayer {
	if limit <= 0 {
		limit = 8
	}
	layers := []wakeLayer{
		{Key: "l0_current_context", Title: "L0 Current Context"},
		{Key: "l1_decisions_constraints", Title: "L1 Decisions And Constraints"},
		{Key: "l2_recent_evidence", Title: "L2 Recent Notes And Evidence"},
	}
	seen := make(map[string]bool)
	total := 0
	add := func(mem store.Memory, fromQuery bool) {
		if total >= limit || seen[mem.ID] {
			return
		}
		if isHistoricalMemory(mem) {
			return
		}
		layerIndex := classifyWakeMemory(mem, query, fromQuery)
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

func classifyWakeMemory(mem store.Memory, query string, fromQuery bool) int {
	role := strings.ToLower(mem.Role)
	sourcePath := strings.ToLower(mem.SourcePath)
	content := strings.ToLower(mem.Content)
	if fromQuery && strings.TrimSpace(query) != "" {
		return 0
	}
	if strings.Contains(sourcePath, "handoff") ||
		strings.Contains(content, "handoff") ||
		strings.Contains(content, "next active task") ||
		strings.Contains(content, "next build step") ||
		strings.Contains(content, "current task") {
		return 0
	}
	switch role {
	case "decision", "adr", "constraint", "preference", "rule":
		return 1
	}
	if strings.Contains(content, "do not ") ||
		strings.Contains(content, "must ") ||
		strings.Contains(content, "non-goal") ||
		strings.Contains(content, "settled decision") {
		return 1
	}
	return 2
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
