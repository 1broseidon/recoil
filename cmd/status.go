package cmd

import (
	"context"
	"fmt"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

type statusResult struct {
	DBPath             string `json:"db_path"`
	ExistedBefore      bool   `json:"existed_before"`
	MemoryCount        int    `json:"memory_count"`
	TombstoneCount     int    `json:"tombstone_count"`
	FTS5               bool   `json:"fts5"`
	ProjectScopeID     string `json:"project_scope_id,omitempty"`
	ProjectRoot        string `json:"project_root,omitempty"`
	ProjectMarker      string `json:"project_marker,omitempty"`
	ProjectInitialized bool   `json:"project_initialized"`
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show database status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.ResolveDBPath(opts.dbPath)
			if err != nil {
				return err
			}
			existed := config.PathExists(dbPath)
			st, _, err := openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			counts, err := st.Counts(context.Background())
			if err != nil {
				return err
			}
			result := statusResult{
				DBPath:         dbPath,
				ExistedBefore:  existed,
				MemoryCount:    counts.Active,
				TombstoneCount: counts.Tombstoned,
				FTS5:           true,
			}
			if projectScope, err := scope.ProjectScope("."); err == nil {
				result.ProjectScopeID = projectScope.ID
				result.ProjectRoot = projectScope.Root
				result.ProjectMarker = projectScope.MarkerPath
				result.ProjectInitialized = projectScope.Initialized
			}

			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, result)
			}
			return frontmatter(w, []kv{
				{k: "db_path", v: dbPath},
				{k: "existed_before", v: fmt.Sprintf("%t", existed)},
				{k: "memory_count", v: fmt.Sprintf("%d", counts.Active)},
				{k: "tombstone_count", v: fmt.Sprintf("%d", counts.Tombstoned)},
				{k: "fts5", v: fmt.Sprintf("%t", result.FTS5)},
				{k: "project_scope_id", v: result.ProjectScopeID},
				{k: "project_root", v: result.ProjectRoot},
				{k: "project_initialized", v: fmt.Sprintf("%t", result.ProjectInitialized)},
				{k: "project_marker", v: result.ProjectMarker},
			}, "ready\n")
		},
	}
}
