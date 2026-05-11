package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type listOptions struct {
	scope          scopeOptions
	filters        memoryFilterOptions
	limit          int
	minimal        bool
	maxChars       int
	includeDeleted bool
}

func newListCommand() *cobra.Command {
	var listOpts listOptions
	c := &cobra.Command{
		Use:   "list",
		Short: "List scoped memories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveReadScope(cmd, listOpts.scope)
			if err != nil {
				return err
			}
			params, err := listParams(sc, listOpts.filters, listOpts.limit, listOpts.includeDeleted)
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			memories, err := st.List(context.Background(), params)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, memories)
			}
			if listOpts.minimal {
				for _, mem := range memories {
					writeMinimalMemory(w, mem, false)
				}
				return nil
			}
			return frontmatter(w, []kv{
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "result_count", v: fmt.Sprintf("%d", len(memories))},
			}, memoryBlocks(memories, listOpts.maxChars, false))
		},
	}
	addScopeFlags(c, &listOpts.scope)
	addMemoryFilterFlags(c, &listOpts.filters)
	c.Flags().IntVar(&listOpts.limit, "limit", 50, "maximum number of memories to list")
	c.Flags().BoolVar(&listOpts.minimal, "minimal", false, "print tab-separated rows")
	c.Flags().IntVar(&listOpts.maxChars, "max-chars", 4000, "maximum characters of memory content to print")
	c.Flags().BoolVar(&listOpts.includeDeleted, "include-deleted", false, "include tombstoned memories")
	return c
}
