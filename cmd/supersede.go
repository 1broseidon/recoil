package cmd

import (
	"context"
	"fmt"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type supersedeOptions struct {
	file       string
	role       string
	agent      string
	sourcePath string
	sourceRef  string
	metadata   string
	claimKey   string
}

type supersedeResult struct {
	OldMemory *store.Memory `json:"old_memory"`
	NewMemory *store.Memory `json:"new_memory"`
	Duplicate bool          `json:"duplicate"`
}

func newSupersedeCommand() *cobra.Command {
	var supersedeOpts supersedeOptions
	c := &cobra.Command{
		Use:   "supersede <old-memory-id> [new text]",
		Short: "Create a new active memory that supersedes an old one",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readAddInput(supersedeOpts.file, args[1:])
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			ctx := context.Background()
			oldMem, err := st.GetMemory(ctx, args[0])
			if err != nil {
				return err
			}
			claimKey := supersedeOpts.claimKey
			if claimKey == "" {
				claimKey = oldMem.ClaimKey
			}
			role := supersedeOpts.role
			if role == "" {
				role = oldMem.Role
			}

			newMem, duplicate, err := st.AddMemory(ctx, store.AddMemoryParams{
				Role:         role,
				Content:      content,
				SourceAgent:  supersedeOpts.agent,
				SourcePath:   supersedeOpts.sourcePath,
				SourceRef:    supersedeOpts.sourceRef,
				ScopeKind:    oldMem.ScopeKind,
				ScopeID:      oldMem.ScopeID,
				ProjectID:    oldMem.ProjectID,
				SessionID:    oldMem.SessionID,
				Room:         oldMem.Room,
				MetadataJSON: supersedeOpts.metadata,
				Validity:     "active",
				ClaimKey:     claimKey,
				Supersedes:   oldMem.ID,
			})
			if err != nil {
				return err
			}
			if newMem.ID == oldMem.ID {
				return fmt.Errorf("replacement memory must differ from the memory it supersedes")
			}
			oldParams := lifecycleParamsFromMemory(*oldMem)
			oldParams.Validity = "superseded"
			oldParams.ClaimKey = claimKey
			oldParams.SupersededBy = newMem.ID
			updatedOld, err := st.UpdateLifecycle(ctx, oldParams)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			result := supersedeResult{
				OldMemory: updatedOld,
				NewMemory: newMem,
				Duplicate: duplicate,
			}
			if opts.json {
				return writeJSON(w, result)
			}
			return frontmatter(w, []kv{
				{k: "action", v: "supersede"},
				{k: "old_id", v: updatedOld.ID},
				{k: "new_id", v: newMem.ID},
				{k: "scope", v: newMem.ScopeKind},
				{k: "scope_id", v: newMem.ScopeID},
				{k: "claim_key", v: claimKey},
				{k: "old_validity", v: updatedOld.Validity},
				{k: "new_validity", v: newMem.Validity},
				{k: "duplicate", v: fmt.Sprintf("%t", duplicate)},
			}, newMem.Content)
		},
	}
	c.Flags().StringVar(&supersedeOpts.file, "file", "", "read replacement memory content from a file, or '-' for stdin")
	c.Flags().StringVar(&supersedeOpts.role, "role", "", "role associated with the replacement memory; defaults to old memory role")
	c.Flags().StringVar(&supersedeOpts.agent, "agent", "", "source agent name")
	c.Flags().StringVar(&supersedeOpts.sourcePath, "source-path", "", "source file or transcript path")
	c.Flags().StringVar(&supersedeOpts.sourceRef, "source-ref", "", "source reference within the path")
	c.Flags().StringVar(&supersedeOpts.metadata, "metadata", "", "custom metadata as JSON")
	c.Flags().StringVar(&supersedeOpts.claimKey, "claim-key", "", "stable claim family for supersession; defaults to old memory claim key")
	return c
}
