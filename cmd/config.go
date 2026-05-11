package cmd

import (
	"fmt"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

type configResult struct {
	DBPath             string `json:"db_path"`
	StateDir           string `json:"state_dir"`
	UserIDPath         string `json:"user_id_path"`
	UserID             string `json:"user_id"`
	DefaultScope       string `json:"default_scope"`
	ProjectInitialized bool   `json:"project_initialized"`
	ProjectRoot        string `json:"project_root,omitempty"`
	ProjectMarker      string `json:"project_marker,omitempty"`
	SQLiteRequiresFTS5 bool   `json:"sqlite_requires_fts5"`
}

func newConfigCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show effective Recoil configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.ResolveDBPath(opts.dbPath)
			if err != nil {
				return err
			}
			stateDir, err := config.ResolveStateDir()
			if err != nil {
				return err
			}
			userIDPath, err := config.ResolveUserIDPath()
			if err != nil {
				return err
			}
			userScope, err := scope.UserScope()
			if err != nil {
				return err
			}
			projectScope, err := scope.ProjectScope(".")
			if err != nil {
				return err
			}
			result := configResult{
				DBPath:             dbPath,
				StateDir:           stateDir,
				UserIDPath:         userIDPath,
				UserID:             userScope.ID,
				DefaultScope:       "project",
				ProjectInitialized: projectScope.Initialized,
				ProjectRoot:        projectScope.Root,
				ProjectMarker:      projectScope.MarkerPath,
				SQLiteRequiresFTS5: true,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "config_result", result)
			}
			return frontmatter(w, []kv{
				{k: "db_path", v: dbPath},
				{k: "state_dir", v: stateDir},
				{k: "user_id_path", v: userIDPath},
				{k: "user_id", v: userScope.ID},
				{k: "default_scope", v: result.DefaultScope},
				{k: "project_initialized", v: fmt.Sprintf("%t", result.ProjectInitialized)},
				{k: "project_root", v: result.ProjectRoot},
				{k: "project_marker", v: result.ProjectMarker},
				{k: "sqlite_requires_fts5", v: fmt.Sprintf("%t", result.SQLiteRequiresFTS5)},
			}, "")
		},
	}
	c.AddCommand(newConfigPathCommand())
	return c
}

func newConfigPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the effective database path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.ResolveDBPath(opts.dbPath)
			if err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), "config_path_result", map[string]string{"db_path": dbPath})
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), dbPath)
			return err
		},
	}
}
