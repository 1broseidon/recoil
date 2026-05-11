package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result := map[string]string{
				"version": version,
				"commit":  commit,
				"date":    date,
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "recoil %s (%s, %s)\n", version, commit, date)
			return err
		},
	}
}
