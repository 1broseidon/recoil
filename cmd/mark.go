package cmd

import (
	"context"
	"fmt"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type markOptions struct {
	validity     string
	claimKey     string
	supersedes   string
	supersededBy string
}

type markResult struct {
	Memory *store.Memory `json:"memory"`
}

func newMarkCommand() *cobra.Command {
	var markOpts markOptions
	c := &cobra.Command{
		Use:   "mark <memory-id>",
		Short: "Update memory lifecycle metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !markLifecycleChanged(cmd) {
				return fmt.Errorf("provide at least one lifecycle flag")
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			existing, err := st.GetMemory(ctx, args[0])
			if err != nil {
				return err
			}
			params := lifecycleParamsFromMemory(*existing)
			if cmd.Flags().Changed("validity") {
				params.Validity = markOpts.validity
			}
			if cmd.Flags().Changed("claim-key") {
				params.ClaimKey = markOpts.claimKey
			}
			if cmd.Flags().Changed("supersedes") {
				params.Supersedes = markOpts.supersedes
			}
			if cmd.Flags().Changed("superseded-by") {
				params.SupersededBy = markOpts.supersededBy
			}

			updated, err := st.UpdateLifecycle(ctx, params)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, markResult{Memory: updated})
			}
			return lifecycleFrontmatter(w, "mark", updated, updated.Content)
		},
	}
	c.Flags().StringVar(&markOpts.validity, "validity", "", "validity state: active, historical, rejected, superseded, stale, unknown")
	c.Flags().StringVar(&markOpts.claimKey, "claim-key", "", "stable claim family for supersession")
	c.Flags().StringVar(&markOpts.supersedes, "supersedes", "", "memory ID this memory supersedes")
	c.Flags().StringVar(&markOpts.supersededBy, "superseded-by", "", "memory ID that supersedes this memory")
	return c
}

func markLifecycleChanged(cmd *cobra.Command) bool {
	for _, flag := range []string{"validity", "claim-key", "supersedes", "superseded-by"} {
		if cmd.Flags().Changed(flag) {
			return true
		}
	}
	return false
}

func lifecycleParamsFromMemory(mem store.Memory) store.LifecycleParams {
	return store.LifecycleParams{
		IDOrPrefix:   mem.ID,
		Validity:     mem.Validity,
		ClaimKey:     mem.ClaimKey,
		Supersedes:   mem.Supersedes,
		SupersededBy: mem.SupersededBy,
	}
}

func lifecycleFrontmatter(w interface {
	Write([]byte) (int, error)
}, action string, mem *store.Memory, content string) error {
	return frontmatter(w, []kv{
		{k: "action", v: action},
		{k: "id", v: mem.ID},
		{k: "scope", v: mem.ScopeKind},
		{k: "scope_id", v: mem.ScopeID},
		{k: "validity", v: mem.Validity},
		{k: "claim_key", v: mem.ClaimKey},
		{k: "supersedes", v: mem.Supersedes},
		{k: "superseded_by", v: mem.SupersededBy},
	}, content)
}
