package cmd

import (
	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type globalOptions struct {
	dbPath string
	json   bool
}

var opts globalOptions

var rootCmd = &cobra.Command{
	Use:   "recoil",
	Short: "Fast local memory recall for agents and humans",
	Long: `Recoil is a local-first CLI memory tool.
It stores verbatim memories in SQLite and recalls them through fast FTS search.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&opts.dbPath, "db", "d", "", "path to recoil database (default: OS app data dir)")
	rootCmd.PersistentFlags().BoolVar(&opts.json, "json", false, "output as JSON")

	rootCmd.AddCommand(newInitCommand())
	rootCmd.AddCommand(newAddCommand())
	rootCmd.AddCommand(newSearchCommand())
	rootCmd.AddCommand(newWakeCommand())
	rootCmd.AddCommand(newShowCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newForgetCommand())
	rootCmd.AddCommand(newMarkCommand())
	rootCmd.AddCommand(newSupersedeCommand())
	rootCmd.AddCommand(newMineCommand())
	rootCmd.AddCommand(newEvalCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newInstructionsCommand())
	rootCmd.AddCommand(newRepairCommand())
	rootCmd.AddCommand(newHookCommand())
	rootCmd.AddCommand(newVersionCommand())
}

func openStore() (*store.Store, string, error) {
	dbPath, err := config.ResolveDBPath(opts.dbPath)
	if err != nil {
		return nil, "", err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, "", err
	}
	return st, dbPath, nil
}
