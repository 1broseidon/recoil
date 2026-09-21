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
	Short:   "Local-first memory for coding agents",
	Version: versionSummary(),
	Long: `recoil keeps a local SQLite store of verbatim memories with provenance: who
wrote each one, from where, in which scope, and whether it still holds. Agents
start with wake, check before acting against a decision, and close with handoff.

Every command takes -d <path> (or RECOIL_DB) to aim at another store and --json
for a structured envelope. Memory commands default to the project you are in.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Help groups. The order here is the order `recoil --help` prints them, and it
// follows the manual: the three calls a session is built on first, then the
// write and read surfaces, then everything an operator wires up once.
const (
	groupSession  = "session"
	groupWrite    = "write"
	groupRead     = "read"
	groupSources  = "sources"
	groupSharing  = "sharing"
	groupAgents   = "agents"
	groupOperator = "operator"
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&opts.dbPath, "db", "d", "", "path to recoil database (default: OS app data dir)")
	rootCmd.PersistentFlags().BoolVar(&opts.json, "json", false, "output as JSON")

	rootCmd.AddGroup(
		&cobra.Group{ID: groupSession, Title: "Session:"},
		&cobra.Group{ID: groupWrite, Title: "Writing memory:"},
		&cobra.Group{ID: groupRead, Title: "Reading memory:"},
		&cobra.Group{ID: groupSources, Title: "Sources:"},
		&cobra.Group{ID: groupSharing, Title: "Sharing:"},
		&cobra.Group{ID: groupAgents, Title: "For agents:"},
		&cobra.Group{ID: groupOperator, Title: "Operator:"},
	)
	addCommands(groupSession, newSetupCommand(), newWakeCommand(), newHandoffCommand())
	addCommands(groupWrite, newRememberCommand(), newDecideCommand(), newAddCommand(), newSupersedeCommand(), newMarkCommand(), newForgetCommand())
	addCommands(groupRead, newSearchCommand(), newCheckCommand(), newListCommand(), newShowCommand(), newClaimsCommand(), newExportCommand(), newProfileCommand())
	addCommands(groupSources, newMineCommand(), newSessionEvidenceCommand(), newEmbedCommand())
	addCommands(groupSharing, newSwarmCommand(), newChannelCommand(), newRelayCommand())
	addCommands(groupAgents, newInstructCommand(), newInstructionsCommand(), newHookCommand(), newMCPCommand())
	addCommands(groupOperator, newInitCommand(), newStatusCommand(), newConfigCommand(), newBackupCommand(), newRepairCommand(), newMigrateCommand(), newEvalCommand(), newTrayCommand(), newVersionCommand())
	rootCmd.SetHelpCommandGroupID(groupOperator)
	rootCmd.SetCompletionCommandGroupID(groupOperator)
}

func addCommands(group string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = group
		rootCmd.AddCommand(c)
	}
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
