package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHookCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "Agent integration hook helpers",
	}
	c.AddCommand(&cobra.Command{
		Use:   "remind",
		Short: "Print a small memory reminder",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			msg := "Recoil: run `recoil wake` at session start and `recoil search \"<topic>\"` before relying on memory.\n"
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"reminder": msg})
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), msg)
			return err
		},
	})
	return c
}
