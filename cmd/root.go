package cmd

import (
	"context"
	"errors"
	"fmt"
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
	Use:     "recoil",
	Short:   "Fast local memory recall for agents and humans",
	Version: versionSummary(),
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
	rootCmd.AddCommand(newSetupCommand())
	rootCmd.AddCommand(newAddCommand())
	rootCmd.AddCommand(newDecideCommand())
	rootCmd.AddCommand(newRememberCommand())
	rootCmd.AddCommand(newClaimsCommand())
	rootCmd.AddCommand(newHandoffCommand())
	rootCmd.AddCommand(newCheckCommand())
	rootCmd.AddCommand(newSearchCommand())
	rootCmd.AddCommand(newWakeCommand())
	rootCmd.AddCommand(newSwarmCommand())
	rootCmd.AddCommand(newShowCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newExportCommand())
	rootCmd.AddCommand(newForgetCommand())
	rootCmd.AddCommand(newMarkCommand())
	rootCmd.AddCommand(newSupersedeCommand())
	rootCmd.AddCommand(newMineCommand())
	rootCmd.AddCommand(newSessionEvidenceCommand())
	rootCmd.AddCommand(newBackupCommand())
	rootCmd.AddCommand(newEmbedCommand())
	rootCmd.AddCommand(newProfileCommand())
	rootCmd.AddCommand(newEvalCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newTrayCommand())
	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newInstructCommand())
	rootCmd.AddCommand(newInstructionsCommand())
	rootCmd.AddCommand(newRepairCommand())
	rootCmd.AddCommand(newMigrateCommand())
	rootCmd.AddCommand(newHookCommand())
	rootCmd.AddCommand(newMCPCommand())
	rootCmd.AddCommand(newChannelCommand())
	rootCmd.AddCommand(newRelayCommand())
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

// readOnlyFallback records why the last openReadStore call had to fall back to a
// writable, migrating open. Tests assert on it; nothing else should depend on it.
type readOnlyFallback struct {
	Path   string
	Reason string
	Schema bool
}

var lastReadOnlyFallback readOnlyFallback

// openReadStore opens the store on a read-only, non-migrating connection.
//
// Read commands use this so a plain read cannot rebuild the FTS index or take a
// write lock. It falls back to openStore (which migrates) on any read-only open
// failure rather than failing the command: a store that does not exist yet, one
// stamped at an older schema version, or a WAL store whose -shm cannot be mapped
// read-only must all still work.
func openReadStore() (*store.Store, string, error) {
	dbPath, err := config.ResolveDBPath(opts.dbPath)
	if err != nil {
		return nil, "", err
	}
	lastReadOnlyFallback = readOnlyFallback{}
	st, roErr := store.OpenReadOnly(dbPath)
	if roErr == nil {
		return st, dbPath, nil
	}
	var schemaErr *store.SchemaVersionError
	isSchema := errors.As(roErr, &schemaErr)
	lastReadOnlyFallback = readOnlyFallback{Path: dbPath, Reason: roErr.Error(), Schema: isSchema}
	// A schema upgrade is a real, one-time event worth announcing. Every other
	// fallback reason is routine, so it stays behind RECOIL_VERBOSE.
	if isSchema {
		fmt.Fprintf(os.Stderr, "notice: upgrading recoil database schema (version %d -> %d) in %s\n", schemaErr.Got, schemaErr.Want, dbPath)
	} else if verboseEnabled() {
		fmt.Fprintf(os.Stderr, "warning: read-only open failed, falling back to a writable open: %v\n", roErr)
	}
	return openStore()
}

// openContextStore is openReadStore for the commands that MAY write as a side
// effect of channel just-in-time refresh (search, check). It stays read-only
// whenever that refresh cannot fire for this command, either because policy
// disables it or because there are no channel subscriptions to import from.
func openContextStore(reason string) (*store.Store, string, error) {
	st, dbPath, err := openReadStore()
	if err != nil {
		return nil, "", err
	}
	if !st.ReadOnly() || !shouldJITRefresh(loadChannelPolicyOrDefault().JITRefresh, reason) {
		return st, dbPath, nil
	}
	subscriptions, subErr := st.ChannelSubscriptions(context.Background())
	if subErr == nil && len(subscriptions) == 0 {
		return st, dbPath, nil
	}
	// Channel refresh will run and needs to write; reopen writable.
	_ = st.Close()
	return openStore()
}

func verboseEnabled() bool {
	return strings.TrimSpace(os.Getenv("RECOIL_VERBOSE")) != ""
}

func dbPathExplicitlySet() bool {
	return strings.TrimSpace(opts.dbPath) != "" || strings.TrimSpace(os.Getenv("RECOIL_DB")) != ""
}

func initializedProjectDBPath() (string, bool, error) {
	sc, err := envAimedProjectScope()
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
