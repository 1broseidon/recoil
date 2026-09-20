package cmd

import (
	"context"
	"fmt"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type repairOptions struct {
	age    bool
	dryRun bool
}

type repairAgeResult struct {
	HandoffsHistorical int  `json:"handoffs_historical"`
	PredicatesStale    int  `json:"predicates_stale"`
	DryRun             bool `json:"dry_run"`
}

func newRepairCommand() *cobra.Command {
	var repairOpts repairOptions
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Rebuild the FTS index and re-run migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := context.Background()
			if repairOpts.age {
				result, err := repairAge(ctx, st, repairOpts.dryRun)
				if err != nil {
					return err
				}
				if opts.json {
					return writeJSON(cmd.OutOrStdout(), "repair_result", result)
				}
				return frontmatter(cmd.OutOrStdout(), []kv{
					{k: "repaired", v: "true"},
					{k: "dry_run", v: fmt.Sprintf("%t", result.DryRun)},
					{k: "handoffs_historical", v: fmt.Sprintf("%d", result.HandoffsHistorical)},
					{k: "predicates_stale", v: fmt.Sprintf("%d", result.PredicatesStale)},
				}, "age repair complete\n")
			}
			if err := st.Repair(ctx); err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "repair_result", map[string]any{"repaired": true})
			}
			return frontmatter(cmd.OutOrStdout(), []kv{{k: "repaired", v: "true"}}, "ready\n")
		},
	}
	cmd.Flags().BoolVar(&repairOpts.age, "age", false, "demote stale handoffs and expired valid_until predicates")
	cmd.Flags().BoolVar(&repairOpts.dryRun, "dry-run", false, "print what would be repaired without writing")
	return cmd
}

func repairAge(ctx context.Context, st *store.Store, dryRun bool) (repairAgeResult, error) {
	result := repairAgeResult{DryRun: dryRun}
	memories, err := st.List(ctx, store.ListParams{Lifecycle: store.LifecycleCurrent, Limit: 1000})
	if err != nil {
		return result, err
	}
	newestByScopeClaim := map[string]store.Memory{}
	for _, mem := range memories {
		if !isDirectHandoff(mem) {
			continue
		}
		key := mem.ScopeKind + "\x00" + mem.ScopeID + "\x00" + mem.ClaimKey
		cur, ok := newestByScopeClaim[key]
		if !ok || mem.CreatedAt > cur.CreatedAt || (mem.CreatedAt == cur.CreatedAt && mem.ID > cur.ID) {
			newestByScopeClaim[key] = mem
		}
	}
	for _, mem := range memories {
		if !isDirectHandoff(mem) {
			continue
		}
		key := mem.ScopeKind + "\x00" + mem.ScopeID + "\x00" + mem.ClaimKey
		if newestByScopeClaim[key].ID == mem.ID {
			continue
		}
		result.HandoffsHistorical++
		if dryRun {
			continue
		}
		if _, err := st.UpdateLifecycle(ctx, store.LifecycleParams{IDOrPrefix: mem.ID, Validity: "superseded", ClaimKey: mem.ClaimKey, Supersedes: mem.Supersedes, SupersededBy: mem.SupersededBy}); err != nil {
			return result, err
		}
	}
	for _, mem := range memories {
		broken, _ := deterministicPredicateBroken(mem)
		if !broken {
			continue
		}
		result.PredicatesStale++
		if dryRun {
			continue
		}
		if _, err := st.UpdateLifecycle(ctx, store.LifecycleParams{IDOrPrefix: mem.ID, Validity: "stale", ClaimKey: mem.ClaimKey, Supersedes: mem.Supersedes, SupersededBy: mem.SupersededBy}); err != nil {
			return result, err
		}
	}
	return result, nil
}
