package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <memory-id>",
		Short: "Show one memory by ID or unique ID prefix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			mem, err := st.GetMemory(context.Background(), args[0])
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "show_result", mem)
			}
			return frontmatter(w, []kv{
				{k: "id", v: mem.ID},
				{k: "scope", v: mem.ScopeKind},
				{k: "scope_id", v: mem.ScopeID},
				{k: "created", v: mem.CreatedAt},
				{k: "validity", v: mem.Validity},
				{k: "claim_key", v: mem.ClaimKey},
				{k: "supersedes", v: mem.Supersedes},
				{k: "superseded_by", v: mem.SupersededBy},
				{k: "role", v: mem.Role},
				{k: "source_kind", v: mem.SourceKind},
				{k: "source_agent", v: mem.SourceAgent},
				{k: "source_path", v: mem.SourcePath},
				{k: "source_ref", v: mem.SourceRef},
			}, fmt.Sprintf("%s\n", mem.Content))
		},
	}
}
