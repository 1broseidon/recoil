package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type forgetOptions struct {
	scope   scopeOptions
	filters memoryFilterOptions
	reason  string
	destroy bool
	dryRun  bool
	force   bool
}

func newForgetCommand() *cobra.Command {
	var forgetOpts forgetOptions
	c := &cobra.Command{
		Use:   "forget [memory-id]",
		Short: "Tombstone or purge memories",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			w := cmd.OutOrStdout()

			if len(args) == 1 {
				result, err := st.ForgetMemory(ctx, forgetParamsFromOptions(args[0], forgetOpts))
				if err != nil {
					return err
				}
				if opts.json {
					return writeJSON(w, "forget_result", result)
				}
				action := "tombstoned"
				if result.Destroyed {
					action = "destroyed"
				}
				return frontmatter(w, []kv{
					{k: "id", v: result.Memory.ID},
					{k: "action", v: action},
					{k: "scope", v: result.Memory.ScopeKind},
					{k: "scope_id", v: result.Memory.ScopeID},
				}, "")
			}

			if !hasBulkForgetFilter(forgetOpts.filters) {
				return fmt.Errorf("bulk forget requires at least one of --agent, --source, --since, or --before")
			}
			sc, err := resolveReadScope(cmd, forgetOpts.scope)
			if err != nil {
				return err
			}
			params, err := listParams(sc, forgetOpts.filters, 1000, forgetOpts.destroy)
			if err != nil {
				return err
			}
			if forgetOpts.dryRun {
				memories, err := st.List(ctx, params)
				if err != nil {
					return err
				}
				ids := make([]string, 0, len(memories))
				for _, mem := range memories {
					ids = append(ids, mem.ID)
				}
				result := map[string]any{
					"ids":       ids,
					"count":     len(ids),
					"destroyed": forgetOpts.destroy,
					"dry_run":   true,
				}
				if opts.json {
					return writeJSON(w, "forget_dry_run_result", result)
				}
				return frontmatter(w, []kv{
					{k: "scope", v: sc.Kind},
					{k: "scope_id", v: sc.ID},
					{k: "dry_run", v: "true"},
					{k: "count", v: fmt.Sprintf("%d", len(ids))},
				}, strings.Join(ids, "\n"))
			}
			if !forgetOpts.force {
				return fmt.Errorf("bulk forget requires --dry-run or --force")
			}
			result, err := st.ForgetByFilter(ctx, params, forgetOpts.reason, forgetOpts.destroy)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(w, "forget_result", result)
			}
			action := "tombstoned"
			if result.Destroyed {
				action = "destroyed"
			}
			return frontmatter(w, []kv{
				{k: "scope", v: sc.Kind},
				{k: "scope_id", v: sc.ID},
				{k: "action", v: action},
				{k: "count", v: fmt.Sprintf("%d", result.Count)},
			}, strings.Join(result.IDs, "\n"))
		},
	}
	addScopeFlags(c, &forgetOpts.scope)
	addMemoryFilterFlags(c, &forgetOpts.filters)
	c.Flags().StringVar(&forgetOpts.reason, "reason", "", "reason for tombstoning")
	c.Flags().BoolVar(&forgetOpts.destroy, "destroy", false, "hard-purge matching memories")
	c.Flags().BoolVar(&forgetOpts.dryRun, "dry-run", false, "preview bulk forget matches")
	c.Flags().BoolVar(&forgetOpts.force, "force", false, "confirm bulk forget")
	return c
}

func forgetParamsFromOptions(id string, opts forgetOptions) store.ForgetParams {
	return store.ForgetParams{
		IDOrPrefix: id,
		Reason:     opts.reason,
		Destroy:    opts.destroy,
	}
}

func hasBulkForgetFilter(filters memoryFilterOptions) bool {
	return filters.agent != "" || filters.sourceKind != "" || filters.source != "" || filters.since != "" || filters.before != ""
}
