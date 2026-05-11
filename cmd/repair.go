package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

func newRepairCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Rebuild the FTS index and re-run migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Repair(context.Background()); err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "repair_result", map[string]any{"repaired": true})
			}
			return frontmatter(cmd.OutOrStdout(), []kv{{k: "repaired", v: "true"}}, "ready\n")
		},
	}
}
