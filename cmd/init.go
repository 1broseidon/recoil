package cmd

import (
	"context"
	"fmt"

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
			result, _, err := runInitProject(context.Background())
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "init_result", result)
			}
			return frontmatter(w, []kv{
				{k: "db_path", v: result.DBPath},
				{k: "state_dir", v: result.StateDir},
				{k: "user_id_path", v: result.UserIDPath},
				{k: "user_id", v: result.UserID},
				{k: "project_root", v: result.ProjectRoot},
				{k: "project_id", v: result.ProjectID},
				{k: "project_marker", v: result.ProjectMarker},
				{k: "fts5", v: fmt.Sprintf("%t", result.FTS5)},
			}, "ready\n")
		},
	}
}
