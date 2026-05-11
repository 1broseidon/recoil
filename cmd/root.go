package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
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
	rootCmd.AddCommand(newDecideCommand())
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
	if err == nil {
		return st, dbPath, nil
	}
	if dbPathExplicitlySet() || !isDBAccessError(err) {
		return nil, "", err
	}
	projectDBPath, ok, fallbackErr := initializedProjectDBPath()
	if fallbackErr != nil || !ok {
		return nil, "", err
	}
	st, fallbackOpenErr := store.Open(projectDBPath)
	if fallbackOpenErr != nil {
		return nil, "", err
	}
	return st, projectDBPath, nil
}

func dbPathExplicitlySet() bool {
	return strings.TrimSpace(opts.dbPath) != "" || strings.TrimSpace(os.Getenv("RECOIL_DB")) != ""
}

func initializedProjectDBPath() (string, bool, error) {
	sc, err := scope.ProjectScope(".")
	if err != nil {
		return "", false, err
	}
	if !sc.Initialized || sc.Root == "" {
		return "", false, nil
	}
	return filepath.Join(sc.Root, scope.ProjectDirName, "recoil.db"), true, nil
}

func isDBAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unable to open database file",
		"operation not permitted",
		"permission denied",
		"no such file or directory",
		"not a directory",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
