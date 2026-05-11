package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type searchOptions struct {
	scope    scopeOptions
	filters  memoryFilterOptions
	limit    int
	minimal  bool
	maxChars int
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

			params, err := searchParams(query, sc, searchOpts.filters, staleAwareFetchLimit(searchOpts.limit))
			if err != nil {
				return err
			}
			results, err := st.Search(context.Background(), params)
			if err != nil {
				return err
			}
			current, historical := splitCurrentHistorical(results)
			current = limitMemories(current, searchOpts.limit)
			historical = limitMemories(historical, searchOpts.limit)

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
	return c
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
