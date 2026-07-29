package cmd

import (
	"fmt"

	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

type migrateOptions struct {
	force bool
}

type migrateResult struct {
	DBPath        string `json:"db_path"`
	SchemaVersion int    `json:"schema_version"`
	Migrated      bool   `json:"migrated"`
	Forced        bool   `json:"forced"`
}

func newMigrateCommand() *cobra.Command {
	var migrateOpts migrateOptions
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Bring the database schema up to date",
		Long: `Open the database with write access so any pending migration runs.

Read commands open the database read-only and refuse to migrate, so they report
an out-of-date schema and point here. Migrating is otherwise automatic: any write
command does it.

Use --force to re-run every migration step, including a full FTS index rebuild,
even when the schema is already current.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, dbPath, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			migrated := st.Migrated()
			if migrateOpts.force {
				if err := st.ForceMigrate(); err != nil {
					return err
				}
				migrated = true
			}
			result := migrateResult{
				DBPath:        dbPath,
				SchemaVersion: store.SchemaVersion,
				Migrated:      migrated,
				Forced:        migrateOpts.force,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "migrate_result", result)
			}
			return frontmatter(w, []kv{
				{k: "db_path", v: result.DBPath},
				{k: "schema_version", v: fmt.Sprintf("%d", result.SchemaVersion)},
				{k: "migrated", v: fmt.Sprintf("%t", result.Migrated)},
				{k: "forced", v: fmt.Sprintf("%t", result.Forced)},
			}, "ready\n")
		},
	}
	c.Flags().BoolVar(&migrateOpts.force, "force", false, "re-run every migration step, including a full FTS index rebuild")
	return c
}
