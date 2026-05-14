package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
	"github.com/spf13/cobra"
)

const (
	hookFormatText       = "text"
	hookFormatJSON       = "json"
	hookFormatClaudeCode = "claude-code"
	hookFormatCodex      = "codex"
)

func newHookCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "Agent integration hook helpers",
		Long: `Agent integration hooks for injecting Recoil memory guidance into
hook-capable tools. Use hook remind directly from agent hooks, or hook install
to wire supported agents.`,
	}
	c.AddCommand(newHookRemindCommand())
	c.AddCommand(newHookInstallCommand(false))
	c.AddCommand(newHookInstallCommand(true))
	return c
}

func newHookRemindCommand() *cobra.Command {
	var format string
	var includeWake bool
	var wakeLimit int
	var maxChars int
	c := &cobra.Command{
		Use:   "remind",
		Short: "Print memory guidance for agent hook injection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reminder := hookReminderText
			contextText := ""
			contextIncluded := false
			if includeWake {
				if found, ok := hookWakeContext(wakeLimit, maxChars); ok {
					contextText = found
					contextIncluded = true
					reminder = strings.TrimSpace(found) + "\n\n" + hookReminderText
				}
			}
			if opts.json && !cmd.Flags().Changed("format") {
				return writeJSON(cmd.OutOrStdout(), "hook_remind_result", map[string]any{
					"reminder":         hookReminderText,
					"context":          contextText,
					"context_included": contextIncluded,
				})
			}
			return emitHookReminderText(cmd.OutOrStdout(), format, reminder)
		},
	}
	c.Flags().StringVar(&format, "format", hookFormatText, "output format: text, json, claude-code, codex")
	c.Flags().BoolVar(&includeWake, "wake", true, "include bounded wake context when an initialized project and readable memory store are available")
	c.Flags().IntVar(&wakeLimit, "limit", 8, "maximum number of wake memories to include when --wake is enabled")
	c.Flags().IntVar(&maxChars, "max-chars", 1600, "maximum characters of wake context to include when --wake is enabled")
	return c
}

func newHookInstallCommand(uninstall bool) *cobra.Command {
	use := "install <agent>"
	short := "Install Recoil hooks into an agent"
	if uninstall {
		use = "uninstall <agent>"
		short = "Remove Recoil hooks from an agent"
	}
	var scope string
	var dryRun bool
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Long: `Supported agents:
  claude-code   native SessionStart/SessionEnd hooks in Claude settings
  opencode      managed OpenCode plugin
  codex         native SessionStart/Stop hooks in Codex hooks.json
  codex-agents  managed AGENTS.md instruction block compatibility fallback`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookInstall(cmd, args[0], scope, dryRun, uninstall)
		},
	}
	c.Flags().StringVar(&scope, "scope", "user", "install scope: user or project")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show intended changes without writing")
	return c
}

const hookReminderText = `Recoil memory guidance:
- At session start in a project, run ` + "`recoil wake --max-chars 1600`" + ` and treat the result as sourced working context.
- Before making or explaining a durable decision, run ` + "`recoil search \"<topic>\"`" + `.
- Before compaction or handoff, store durable conclusions with ` + "`recoil add --agent <agent> --role decision \"<memory>\"`" + `.
- If Session Evidence is enabled and supported hooks are installed, session-end evidence is captured as selected redacted evidence, not raw transcript storage.
- Use ` + "`--user`" + ` only for cross-project preferences, and ` + "`--session <id>`" + ` for one-session memories.
- Treat Recoil results as evidence with IDs and provenance, not unquestionable truth.`

func emitHookReminder(w io.Writer, format string) error {
	return emitHookReminderText(w, format, hookReminderText)
}

func emitHookReminderText(w io.Writer, format, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = hookReminderText
	}
	switch format {
	case "", hookFormatText:
		_, err := fmt.Fprintln(w, text)
		return err
	case hookFormatJSON:
		return writeRawJSON(w, map[string]string{"systemMessage": text})
	case hookFormatClaudeCode, hookFormatCodex:
		return writeRawJSON(w, map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "SessionStart",
				"additionalContext": text,
			},
		})
	default:
		return fmt.Errorf("unknown --format %q (want: text, json, claude-code, codex)", format)
	}
}

func hookWakeContext(limit, maxChars int) (string, bool) {
	if limit <= 0 {
		limit = 8
	}
	if maxChars <= 0 {
		maxChars = 1600
	}
	sc, err := scope.ProjectScope(".")
	if err != nil || !sc.Initialized {
		return "", false
	}
	dbPath, ok := hookReadableDBPath(sc)
	if !ok {
		return "", false
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return "", false
	}
	defer st.Close()

	_, settings, err := loadProjectSettings()
	if err != nil {
		return "", false
	}
	quality := effectiveSourceQualityOptions(settings)
	ctx := context.Background()
	recent, err := wakeRecentMemories(ctx, st, sc, memoryFilterOptions{}, wakeFetchLimit(limit), limit, quality)
	if err != nil {
		return "", false
	}
	layers := buildWakeLayers("", nil, recent, limit, quality)
	if len(flattenWakeLayers(layers)) == 0 {
		return "", false
	}
	rendered := layeredMemoryBlocks(layers, maxChars, true)
	if rendered.ShownCount == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("Recoil wake context:\n")
	b.WriteString(strings.TrimSpace(rendered.Body))
	if rendered.Truncated {
		b.WriteString("\n\n[truncated]")
	}
	return b.String(), true
}

func hookReadableDBPath(sc scope.Scope) (string, bool) {
	if dbPathExplicitlySet() {
		dbPath, err := config.ResolveDBPath(opts.dbPath)
		if err != nil || !config.PathExists(dbPath) {
			return "", false
		}
		return dbPath, true
	}
	dbPath, err := config.ResolveDBPath("")
	if err == nil && config.PathExists(dbPath) {
		return dbPath, true
	}
	if sc.Root != "" {
		projectDB := filepath.Join(sc.Root, scope.ProjectDirName, "recoil.db")
		if config.PathExists(projectDB) {
			return projectDB, true
		}
	}
	return "", false
}

func writeRawJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(data)
}

type hookAdapter struct {
	install   func(scope string, dryRun bool) (target, summary string, err error)
	uninstall func(scope string, dryRun bool) (target, summary string, err error)
}

func runHookInstall(cmd *cobra.Command, agent, scope string, dryRun, uninstall bool) error {
	if scope != "user" && scope != "project" {
		return fmt.Errorf("--scope must be 'user' or 'project'")
	}
	adapter, err := lookupHookAdapter(agent)
	if err != nil {
		return err
	}
	action := adapter.install
	verb := "installed"
	if uninstall {
		action = adapter.uninstall
		verb = "removed"
	}
	target, summary, err := action(scope, dryRun)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would update %s\n---\n%s\n", target, summary)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "recoil hooks %s for %s (%s scope) -> %s\n", verb, agent, scope, target)
	return nil
}

func lookupHookAdapter(agent string) (hookAdapter, error) {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude", "claude-code", "claudecode":
		return hookAdapter{install: installClaudeCodeHooks, uninstall: uninstallClaudeCodeHooks}, nil
	case "opencode":
		return hookAdapter{install: installOpenCodeHooks, uninstall: uninstallOpenCodeHooks}, nil
	case "codex":
		return hookAdapter{install: installCodexHooks, uninstall: uninstallCodexHooks}, nil
	case "codex-agents", "codex-agent", "codex-instructions":
		return hookAdapter{install: installCodexInstructionHooks, uninstall: uninstallCodexInstructionHooks}, nil
	default:
		return hookAdapter{}, fmt.Errorf("unknown agent %q (supported: claude-code, opencode, codex, codex-agents)", agent)
	}
}

const (
	recoilHookMarker       = "recoil-hook"
	claudeHookCommand      = "recoil hook remind --format=claude-code"
	claudeEvidenceCommand  = "recoil session-evidence hook --agent claude-code"
	opencodePluginName     = "recoil-opencode.js"
	opencodePluginPrefix   = "// recoil-hook managed by recoil\n// recoil-version: "
	codexHookCommand       = "recoil hook remind --format=codex"
	codexEvidenceCommand   = "recoil session-evidence hook --agent codex"
	codexHookStatusMessage = "Loading Recoil memory guidance"
	codexEvidenceStatus    = "Capturing Recoil session evidence"
	codexManagedBlockOpen  = "<!-- recoil-hook:start -->"
	codexManagedBlockEnd   = "<!-- recoil-hook:end -->"
)

// Claude Code adapter.

type claudeSettings struct {
	raw map[string]any
}

func claudeSettingsPath(scope string) (string, error) {
	if scope == "project" {
		return filepath.Join(".claude", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func loadClaudeSettings(path string) (*claudeSettings, error) {
	settings := &claudeSettings{raw: map[string]any{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings.raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return settings, nil
}

func writeClaudeSettings(path string, settings *claudeSettings) error {
	data, err := json.MarshalIndent(settings.raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

func mergeClaudeHooks(settings *claudeSettings) {
	removeClaudeHooks(settings)
	hooks, _ := settings.raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["SessionStart"] = appendUniqueMarkedHook(hooks["SessionStart"], map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": claudeHookCommand,
				"marker":  recoilHookMarker,
				"timeout": 5,
			},
		},
	})
	hooks["SessionEnd"] = appendUniqueMarkedHook(hooks["SessionEnd"], map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": claudeEvidenceCommand,
				"marker":  recoilHookMarker,
				"timeout": 30,
			},
		},
	})
	settings.raw["hooks"] = hooks
}

func removeClaudeHooks(settings *claudeSettings) {
	hooks, _ := settings.raw["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, key := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse"} {
		arr, _ := hooks[key].([]any)
		if arr == nil {
			continue
		}
		filtered := arr[:0]
		for _, entry := range arr {
			next, keep := removeMarkedHooksFromGroup(entry, recoilHookMarker)
			if keep {
				filtered = append(filtered, next)
			}
		}
		if len(filtered) == 0 {
			delete(hooks, key)
		} else {
			hooks[key] = filtered
		}
	}
	if len(hooks) == 0 {
		delete(settings.raw, "hooks")
	}
}

func removeMarkedHooksFromGroup(entry any, marker string) (any, bool) {
	group, ok := entry.(map[string]any)
	if !ok {
		return entry, true
	}
	hooks, ok := group["hooks"].([]any)
	if !ok {
		return entry, true
	}
	filtered := hooks[:0]
	removed := false
	for _, hook := range hooks {
		item, _ := hook.(map[string]any)
		if item != nil && item["marker"] == marker {
			removed = true
			continue
		}
		filtered = append(filtered, hook)
	}
	if !removed {
		return entry, true
	}
	if len(filtered) == 0 {
		return nil, false
	}
	next := map[string]any{}
	for k, v := range group {
		next[k] = v
	}
	next["hooks"] = filtered
	return next, true
}

func appendUniqueMarkedHook(existing any, group map[string]any) []any {
	arr, _ := existing.([]any)
	for _, entry := range arr {
		if hookGroupHasMarker(entry, recoilHookMarker) {
			return arr
		}
	}
	return append(arr, group)
}

func hookGroupHasMarker(entry any, marker string) bool {
	group, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, _ := group["hooks"].([]any)
	for _, hook := range hooks {
		item, _ := hook.(map[string]any)
		if item != nil && item["marker"] == marker {
			return true
		}
	}
	return false
}

func installClaudeCodeHooks(scope string, dryRun bool) (string, string, error) {
	path, err := claudeSettingsPath(scope)
	if err != nil {
		return "", "", err
	}
	settings, err := loadClaudeSettings(path)
	if err != nil {
		return path, "", err
	}
	mergeClaudeHooks(settings)
	data, _ := json.MarshalIndent(settings.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeClaudeSettings(path, settings); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

func uninstallClaudeCodeHooks(scope string, dryRun bool) (string, string, error) {
	path, err := claudeSettingsPath(scope)
	if err != nil {
		return "", "", err
	}
	settings, err := loadClaudeSettings(path)
	if err != nil {
		return path, "", err
	}
	removeClaudeHooks(settings)
	data, _ := json.MarshalIndent(settings.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeClaudeSettings(path, settings); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

// OpenCode adapter.

func opencodePluginPath(scope string) (string, error) {
	if scope == "project" {
		return filepath.Join(".opencode", "plugins", opencodePluginName), nil
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, "opencode", "plugins", opencodePluginName), nil
}

func openCodePluginContents() string {
	return opencodePluginPrefix + hookPluginVersion() + `

export default async ({ $ }) => ({
  "experimental.chat.system.transform": async (_input, output) => {
    try {
      const reminder = await $` + "`" + `recoil hook remind --format=text` + "`" + `.text()
      const text = reminder.trim()
      if (text) output.system.push(text)
    } catch (error) {
      void error
    }
  },
})
`
}

func hookPluginVersion() string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "dev"
	}
	return v
}

func installOpenCodeHooks(scope string, dryRun bool) (string, string, error) {
	path, err := opencodePluginPath(scope)
	if err != nil {
		return "", "", err
	}
	content := openCodePluginContents()
	if managed, err := managedFileState(path, opencodePluginPrefix); err != nil {
		return path, "", err
	} else if !managed {
		return path, "", fmt.Errorf("refusing to overwrite non-Recoil OpenCode plugin at %s", path)
	}
	if dryRun {
		return path, content, nil
	}
	if err := atomicWriteFile(path, []byte(content), 0o644); err != nil {
		return path, "", err
	}
	return path, content, nil
}

func uninstallOpenCodeHooks(scope string, dryRun bool) (string, string, error) {
	path, err := opencodePluginPath(scope)
	if err != nil {
		return "", "", err
	}
	managed, err := managedFileState(path, opencodePluginPrefix)
	if err != nil {
		return path, "", err
	}
	if !managed {
		return path, "leave non-Recoil OpenCode plugin untouched", nil
	}
	if dryRun {
		return path, "remove managed Recoil OpenCode plugin", nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, "", err
	}
	return path, "remove managed Recoil OpenCode plugin", nil
}

func managedFileState(path, prefix string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return strings.HasPrefix(string(data), prefix), nil
}

// Codex adapter.

type codexHooksSettings struct {
	raw map[string]any
}

func codexHooksPath(scope string) (string, error) {
	if scope == "project" {
		return filepath.Join(".codex", "hooks.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

func loadCodexHooks(path string) (*codexHooksSettings, error) {
	settings := &codexHooksSettings{raw: map[string]any{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings.raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return settings, nil
}

func writeCodexHooks(path string, settings *codexHooksSettings) error {
	data, err := json.MarshalIndent(settings.raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

func mergeCodexHooks(settings *codexHooksSettings) {
	removeCodexHooks(settings)
	hooks, _ := settings.raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["SessionStart"] = append(hookGroups(hooks["SessionStart"]), map[string]any{
		"matcher": "startup|resume|clear",
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       codexHookCommand,
				"statusMessage": codexHookStatusMessage,
				"timeout":       5,
			},
		},
	})
	hooks["Stop"] = append(hookGroups(hooks["Stop"]), map[string]any{
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       codexEvidenceCommand,
				"statusMessage": codexEvidenceStatus,
				"timeout":       30,
			},
		},
	})
	settings.raw["hooks"] = hooks
}

func removeCodexHooks(settings *codexHooksSettings) {
	hooks, _ := settings.raw["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, key := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreToolUse", "PermissionRequest", "PostToolUse"} {
		arr, _ := hooks[key].([]any)
		if arr == nil {
			continue
		}
		filtered := arr[:0]
		for _, entry := range arr {
			next := entry
			keep := true
			for _, command := range []string{codexHookCommand, codexEvidenceCommand} {
				next, keep = removeCommandHooksFromGroup(next, command)
				if !keep {
					break
				}
			}
			if keep {
				filtered = append(filtered, next)
			}
		}
		if len(filtered) == 0 {
			delete(hooks, key)
		} else {
			hooks[key] = filtered
		}
	}
	if len(hooks) == 0 {
		delete(settings.raw, "hooks")
	}
}

func hookGroups(existing any) []any {
	arr, _ := existing.([]any)
	return arr
}

func removeCommandHooksFromGroup(entry any, command string) (any, bool) {
	group, ok := entry.(map[string]any)
	if !ok {
		return entry, true
	}
	hooks, ok := group["hooks"].([]any)
	if !ok {
		return entry, true
	}
	filtered := hooks[:0]
	removed := false
	for _, hook := range hooks {
		item, _ := hook.(map[string]any)
		if item != nil && item["command"] == command {
			removed = true
			continue
		}
		filtered = append(filtered, hook)
	}
	if !removed {
		return entry, true
	}
	if len(filtered) == 0 {
		return nil, false
	}
	next := map[string]any{}
	for k, v := range group {
		next[k] = v
	}
	next["hooks"] = filtered
	return next, true
}

func installCodexHooks(scope string, dryRun bool) (string, string, error) {
	path, err := codexHooksPath(scope)
	if err != nil {
		return "", "", err
	}
	settings, err := loadCodexHooks(path)
	if err != nil {
		return path, "", err
	}
	mergeCodexHooks(settings)
	data, _ := json.MarshalIndent(settings.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeCodexHooks(path, settings); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

func uninstallCodexHooks(scope string, dryRun bool) (string, string, error) {
	path, err := codexHooksPath(scope)
	if err != nil {
		return "", "", err
	}
	settings, err := loadCodexHooks(path)
	if err != nil {
		return path, "", err
	}
	removeCodexHooks(settings)
	data, _ := json.MarshalIndent(settings.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeCodexHooks(path, settings); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

// Codex AGENTS.md compatibility adapter.

func codexInstructionsPath(scope string) (string, error) {
	if scope == "project" {
		return "AGENTS.md", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "AGENTS.md"), nil
}

func codexManagedBlock() string {
	return codexManagedBlockOpen + `
# Recoil memory

At session start in this project, run ` + "`recoil hook remind`" + ` and follow it as persistent memory guidance. In particular, run ` + "`recoil wake --max-chars 1600`" + ` before starting substantive work, run ` + "`recoil search \"<topic>\"`" + ` before relying on memory for durable decisions, and store durable conclusions with ` + "`recoil add --agent codex --role decision \"<memory>\"`" + `.
` + codexManagedBlockEnd + "\n"
}

func installCodexInstructionHooks(scope string, dryRun bool) (string, string, error) {
	path, err := codexInstructionsPath(scope)
	if err != nil {
		return "", "", err
	}
	existing, err := readOptionalText(path)
	if err != nil {
		return path, "", err
	}
	next := replaceManagedBlock(existing, codexManagedBlock())
	if dryRun {
		return path, next, nil
	}
	if err := atomicWriteFile(path, []byte(next), 0o644); err != nil {
		return path, "", err
	}
	return path, next, nil
}

func uninstallCodexInstructionHooks(scope string, dryRun bool) (string, string, error) {
	path, err := codexInstructionsPath(scope)
	if err != nil {
		return "", "", err
	}
	existing, err := readOptionalText(path)
	if err != nil {
		return path, "", err
	}
	next := removeManagedBlock(existing)
	if dryRun {
		return path, next, nil
	}
	if err := atomicWriteFile(path, []byte(next), 0o644); err != nil {
		return path, "", err
	}
	return path, next, nil
}

func readOptionalText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func replaceManagedBlock(existing, block string) string {
	without := strings.TrimRight(removeManagedBlock(existing), "\n")
	if without == "" {
		return block
	}
	return without + "\n\n" + block
}

func removeManagedBlock(existing string) string {
	for {
		start := strings.Index(existing, codexManagedBlockOpen)
		if start < 0 {
			return existing
		}
		end := strings.Index(existing[start:], codexManagedBlockEnd)
		if end < 0 {
			return existing
		}
		end += start + len(codexManagedBlockEnd)
		for end < len(existing) && (existing[end] == '\n' || existing[end] == '\r') {
			end++
		}
		existing = existing[:start] + existing[end:]
	}
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Chmod(path, mode)
}
