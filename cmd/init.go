package cmd

import (
	"context"
	"fmt"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

type initResult struct {
	DBPath        string `json:"db_path"`
	StateDir      string `json:"state_dir"`
	UserIDPath    string `json:"user_id_path"`
	UserID        string `json:"user_id"`
	ProjectRoot   string `json:"project_root"`
	ProjectID     string `json:"project_id"`
	ProjectMarker string `json:"project_marker"`
	FTS5          bool   `json:"fts5"`
}

func newInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize local Recoil state",
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
			projectScope, err := scope.InitProject(".")
			if err != nil {
				return err
			}
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Repair(context.Background()); err != nil {
				return err
			}
			result := initResult{
				DBPath:        dbPath,
				StateDir:      stateDir,
				UserIDPath:    userIDPath,
				UserID:        userScope.ID,
				ProjectRoot:   projectScope.Root,
				ProjectID:     projectScope.ProjectID,
				ProjectMarker: projectScope.MarkerPath,
				FTS5:          true,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, result)
			}
			return frontmatter(w, []kv{
				{k: "db_path", v: dbPath},
				{k: "state_dir", v: stateDir},
				{k: "user_id_path", v: userIDPath},
				{k: "user_id", v: userScope.ID},
				{k: "project_root", v: projectScope.Root},
				{k: "project_id", v: projectScope.ProjectID},
				{k: "project_marker", v: projectScope.MarkerPath},
				{k: "fts5", v: fmt.Sprintf("%t", result.FTS5)},
			}, "ready\n")
		},
	}
}
