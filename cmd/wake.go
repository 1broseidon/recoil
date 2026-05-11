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
	Query   string         `json:"query,omitempty"`
	Scope   string         `json:"scope"`
	ScopeID string         `json:"scope_id"`
	Results []store.Memory `json:"results"`
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
			results := make([]store.Memory, 0, wakeOpts.limit)
			seen := make(map[string]bool)
			if query != "" {
				params, err := searchParams(query, sc, wakeOpts.filters, wakeOpts.limit)
				if err != nil {
					return err
				}
				found, err := st.Search(ctx, params)
				if err != nil {
					return err
				}
				appendUnique(&results, seen, found, wakeOpts.limit)
			}

			params, err := listParams(sc, wakeOpts.filters, wakeOpts.limit, false)
			if err != nil {
				return err
			}
			recent, err := st.List(ctx, params)
			if err != nil {
				return err
			}
			appendUnique(&results, seen, recent, wakeOpts.limit)

			w := cmd.OutOrStdout()
			result := wakeResult{
				Query:   query,
				Scope:   sc.Kind,
				ScopeID: sc.ID,
				Results: results,
			}
			if opts.json {
				return writeJSON(w, result)
			}
			if wakeOpts.minimal {
				for _, mem := range results {
					writeMinimalMemory(w, mem, mem.Score > 0)
				}
				return nil
			}
			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(results))},
				{k: "max_chars", v: fmt.Sprintf("%d", wakeOpts.maxChars)},
			}, memoryBlocks(results, wakeOpts.maxChars, true))
		},
	}
	addScopeFlags(c, &wakeOpts.scope)
	addMemoryFilterFlags(c, &wakeOpts.filters)
	c.Flags().IntVar(&wakeOpts.limit, "limit", 8, "maximum number of memories to include")
	c.Flags().IntVar(&wakeOpts.maxChars, "max-chars", 1600, "maximum characters of memory content to print")
	c.Flags().BoolVar(&wakeOpts.minimal, "minimal", false, "print tab-separated rows")
	return c
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
