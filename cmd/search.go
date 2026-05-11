package cmd

import (
	"context"
	"fmt"
	"strings"

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

			params, err := searchParams(query, sc, searchOpts.filters, searchOpts.limit)
			if err != nil {
				return err
			}
			results, err := st.Search(context.Background(), params)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, results)
			}
			if searchOpts.minimal {
				for _, r := range results {
					writeMinimalMemory(w, r, true)
				}
				return nil
			}

			return frontmatter(w, []kv{
				{k: "query", v: query},
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(results))},
			}, memoryBlocks(results, searchOpts.maxChars, true))
		},
	}
	addScopeFlags(c, &searchOpts.scope)
	addMemoryFilterFlags(c, &searchOpts.filters)
	c.Flags().IntVar(&searchOpts.limit, "limit", 5, "maximum number of memories to return")
	c.Flags().BoolVar(&searchOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().IntVar(&searchOpts.maxChars, "max-chars", 4000, "maximum characters of memory content to print")
	return c
}
