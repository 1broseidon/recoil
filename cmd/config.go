package cmd

import (
	"fmt"
	"sort"
	"strings"

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
	SettingsPath       string `json:"settings_path"`
	SessionEvidence    bool   `json:"session_evidence_enabled"`
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
			settingsPath, err := config.ResolveSettingsPath(projectScope.Root)
			if err != nil {
				return err
			}
			settings, err := config.LoadSettings(settingsPath)
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
				SettingsPath:       settingsPath,
				SessionEvidence:    settings.Bool("session-evidence.enabled", false),
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
				{k: "settings_path", v: result.SettingsPath},
				{k: "session_evidence_enabled", v: fmt.Sprintf("%t", result.SessionEvidence)},
				{k: "sqlite_requires_fts5", v: fmt.Sprintf("%t", result.SQLiteRequiresFTS5)},
			}, "")
		},
	}
	c.AddCommand(newConfigPathCommand())
	c.AddCommand(newConfigGetCommand())
	c.AddCommand(newConfigSetCommand())
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

func newConfigGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "Get Recoil configuration values",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(args) == 1 {
				value, ok := settings.Get(args[0])
				if opts.json {
					return writeJSON(w, "config_get_result", map[string]any{"path": path, "key": args[0], "value": value, "found": ok})
				}
				if !ok {
					return fmt.Errorf("config key %q is not set", args[0])
				}
				_, err = fmt.Fprintln(w, value)
				return err
			}
			keys := make([]string, 0, len(settings.Values))
			for key := range settings.Values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if opts.json {
				return writeJSON(w, "config_get_result", map[string]any{"path": path, "values": settings.Values})
			}
			var lines []string
			for _, key := range keys {
				lines = append(lines, key+"="+settings.Values[key])
			}
			return frontmatter(w, []kv{{k: "path", v: path}, {k: "count", v: fmt.Sprintf("%d", len(keys))}}, strings.Join(lines, "\n"))
		},
	}
}

func newConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a Recoil configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			settings.Set(args[0], args[1])
			if err := config.SaveSettings(path, settings); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "config_set_result", map[string]string{"path": path, "key": args[0], "value": args[1]})
			}
			return frontmatter(w, []kv{{k: "path", v: path}, {k: "key", v: args[0]}, {k: "value", v: args[1]}}, "")
		},
	}
}

func loadProjectSettings() (string, config.Settings, error) {
	sc, err := scope.ProjectScope(".")
	if err != nil {
		return "", config.Settings{}, err
	}
	path, err := config.ResolveSettingsPath(sc.Root)
	if err != nil {
		return "", config.Settings{}, err
	}
	settings, err := config.LoadSettings(path)
	return path, settings, err
}
