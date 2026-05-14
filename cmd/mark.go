package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type markOptions struct {
	validity     string
	claimKey     string
	supersedes   string
	supersededBy string
	stance       string
	subject      string
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
			if !markChanged(cmd) {
				return fmt.Errorf("provide at least one lifecycle or decision metadata flag")
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

			updated := existing
			if markLifecycleChanged(cmd) {
				updated, err = st.UpdateLifecycle(ctx, params)
				if err != nil {
					return err
				}
			}
			if markDecisionMetadataChanged(cmd) {
				stance := markOpts.stance
				subject := markOpts.subject
				existingDecision := decisionMetadataFromMemory(*updated)
				if !cmd.Flags().Changed("stance") {
					stance = existingDecision.Stance
				}
				if !cmd.Flags().Changed("subject") {
					subject = existingDecision.Subject
				}
				if strings.TrimSpace(stance) == "" || strings.TrimSpace(subject) == "" {
					return fmt.Errorf("--stance and --subject must both be set after marking")
				}
				metadata, err := mergeDecisionMetadata(updated.MetadataJSON, stance, subject)
				if err != nil {
					return err
				}
				updated, err = st.UpdateMetadata(ctx, store.MetadataParams{IDOrPrefix: updated.ID, MetadataJSON: metadata})
				if err != nil {
					return err
				}
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "mark_result", markResult{Memory: updated})
			}
			return lifecycleFrontmatter(w, "mark", updated, updated.Content)
		},
	}
	c.Flags().StringVar(&markOpts.validity, "validity", "", "validity state: active, historical, rejected, superseded, stale, unknown")
	c.Flags().StringVar(&markOpts.claimKey, "claim-key", "", "stable claim family for supersession")
	c.Flags().StringVar(&markOpts.supersedes, "supersedes", "", "memory ID this memory supersedes")
	c.Flags().StringVar(&markOpts.supersededBy, "superseded-by", "", "memory ID that supersedes this memory")
	c.Flags().StringVar(&markOpts.stance, "stance", "", "decision stance: prefers, rejects, requires, forbids")
	c.Flags().StringVar(&markOpts.subject, "subject", "", "decision subject used by check opposition detection")
	return c
}

func markChanged(cmd *cobra.Command) bool {
	return markLifecycleChanged(cmd) || markDecisionMetadataChanged(cmd)
}

func markLifecycleChanged(cmd *cobra.Command) bool {
	for _, flag := range []string{"validity", "claim-key", "supersedes", "superseded-by"} {
		if cmd.Flags().Changed(flag) {
			return true
		}
	}
	return false
}

func markDecisionMetadataChanged(cmd *cobra.Command) bool {
	for _, flag := range []string{"stance", "subject"} {
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
		{k: "stance", v: decisionMetadataFromMemory(*mem).Stance},
		{k: "subject", v: decisionMetadataFromMemory(*mem).Subject},
		{k: "supersedes", v: mem.Supersedes},
		{k: "superseded_by", v: mem.SupersededBy},
	}, content)
}
