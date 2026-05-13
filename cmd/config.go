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
	DBPath             string            `json:"db_path"`
	StateDir           string            `json:"state_dir"`
	UserIDPath         string            `json:"user_id_path"`
	UserID             string            `json:"user_id"`
	DefaultScope       string            `json:"default_scope"`
	ProjectInitialized bool              `json:"project_initialized"`
	ProjectRoot        string            `json:"project_root,omitempty"`
	ProjectMarker      string            `json:"project_marker,omitempty"`
	SettingsPath       string            `json:"settings_path"`
	SessionEvidence    bool              `json:"session_evidence_enabled"`
	SQLiteRequiresFTS5 bool              `json:"sqlite_requires_fts5"`
	Settings           map[string]string `json:"settings"`
}

type configEntry struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Source      string `json:"source"`
	Type        string `json:"type,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
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
				Settings:           config.EffectiveValues(settings),
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
				{k: "settings", v: fmt.Sprintf("%d", len(result.Settings))},
			}, configEntriesText(settings, false))
		},
	}
	c.AddCommand(newConfigPathCommand())
	c.AddCommand(newConfigGetCommand())
	c.AddCommand(newConfigSetCommand())
	c.AddCommand(newConfigUnsetCommand())
	c.AddCommand(newConfigListCommand())
	c.AddCommand(newConfigExplainCommand())
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
				value, source, ok := config.EffectiveValue(settings, args[0])
				if opts.json {
					return writeJSON(w, "config_get_result", map[string]any{"path": path, "key": args[0], "value": value, "source": source, "found": ok})
				}
				if !ok {
					return fmt.Errorf("config key %q is not set", args[0])
				}
				_, err = fmt.Fprintln(w, value)
				return err
			}
			if opts.json {
				return writeJSON(w, "config_get_result", map[string]any{"path": path, "values": config.EffectiveValues(settings)})
			}
			return frontmatter(w, []kv{{k: "path", v: path}, {k: "count", v: fmt.Sprintf("%d", len(config.EffectiveValues(settings)))}}, configEntriesText(settings, false))
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
			if err := config.Validate(args[0], args[1]); err != nil {
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

func newConfigUnsetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a project configuration override",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			if _, ok := settings.Get(args[0]); !ok {
				if _, known := config.DefinitionForKey(args[0]); !known {
					return fmt.Errorf("config key %q is not set", args[0])
				}
			}
			settings.Unset(args[0])
			if err := config.SaveSettings(path, settings); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "config_unset_result", map[string]string{"path": path, "key": args[0]})
			}
			return frontmatter(w, []kv{{k: "path", v: path}, {k: "key", v: args[0]}}, "")
		},
	}
}

func newConfigListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List effective Recoil configuration values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			entries := configEntries(settings, true)
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "config_list_result", map[string]any{"path": path, "entries": entries})
			}
			return frontmatter(w, []kv{{k: "path", v: path}, {k: "count", v: fmt.Sprintf("%d", len(entries))}}, configEntriesText(settings, true))
		},
	}
}

func newConfigExplainCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <key>",
		Short: "Explain a Recoil configuration key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, settings, err := loadProjectSettings()
			if err != nil {
				return err
			}
			def, ok := config.DefinitionForKey(args[0])
			if !ok {
				return fmt.Errorf("unknown config key %q", args[0])
			}
			value, source, _ := config.EffectiveValue(settings, args[0])
			entry := configEntry{
				Key:         args[0],
				Value:       value,
				Source:      source,
				Type:        def.Type,
				Default:     def.Default,
				Description: def.Description,
			}
			w := cmd.OutOrStdout()
			if opts.json {
				return writeJSON(w, "config_explain_result", map[string]any{"path": path, "entry": entry})
			}
			return frontmatter(w, []kv{
				{k: "path", v: path},
				{k: "key", v: entry.Key},
				{k: "type", v: entry.Type},
				{k: "default", v: entry.Default},
				{k: "value", v: entry.Value},
				{k: "source", v: entry.Source},
			}, entry.Description)
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

func configEntries(settings config.Settings, includeDescriptions bool) []configEntry {
	values := config.EffectiveValues(settings)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]configEntry, 0, len(keys))
	for _, key := range keys {
		value, source, _ := config.EffectiveValue(settings, key)
		entry := configEntry{Key: key, Value: value, Source: source}
		if def, ok := config.DefinitionForKey(key); ok {
			entry.Type = def.Type
			entry.Default = def.Default
			if includeDescriptions {
				entry.Description = def.Description
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func configEntriesText(settings config.Settings, includeDescriptions bool) string {
	entries := configEntries(settings, includeDescriptions)
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := entry.Key + "=" + entry.Value
		if entry.Source != "" {
			line += "\t" + entry.Source
		}
		if includeDescriptions && entry.Description != "" {
			line += "\t" + entry.Description
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
