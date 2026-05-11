package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookRemindFormats(t *testing.T) {
	var text bytes.Buffer
	if err := emitHookReminder(&text, hookFormatText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "recoil wake --max-chars 1600") {
		t.Fatalf("expected wake guidance, got:\n%s", text.String())
	}

	var generic bytes.Buffer
	if err := emitHookReminder(&generic, hookFormatJSON); err != nil {
		t.Fatal(err)
	}
	var genericPayload struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(generic.Bytes(), &genericPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(genericPayload.SystemMessage, "recoil search") {
		t.Fatalf("expected generic JSON systemMessage, got:\n%s", generic.String())
	}

	var claude bytes.Buffer
	if err := emitHookReminder(&claude, hookFormatClaudeCode); err != nil {
		t.Fatal(err)
	}
	var claudePayload struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal(claude.Bytes(), &claudePayload); err != nil {
		t.Fatal(err)
	}
	if claudePayload.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Fatalf("expected Claude SessionStart payload, got:\n%s", claude.String())
	}
	if !strings.Contains(claudePayload.HookSpecificOutput.AdditionalContext, "Recoil memory guidance") {
		t.Fatalf("expected Claude additionalContext, got:\n%s", claude.String())
	}
	if claudePayload.SystemMessage != "" {
		t.Fatalf("did not expect generic systemMessage in Claude payload, got:\n%s", claude.String())
	}
}

func TestHookRemindGlobalJSONUsesEnvelope(t *testing.T) {
	oldOpts := opts
	opts = globalOptions{json: true}
	defer func() { opts = oldOpts }()

	c := newHookRemindCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope["kind"]) != `"hook_remind_result"` {
		t.Fatalf("expected hook_remind_result kind, got:\n%s", out.String())
	}
	if _, ok := envelope["data"]; !ok {
		t.Fatalf("expected data envelope, got:\n%s", out.String())
	}
}

func TestClaudeCodeHooksAreIdempotentAndPreserveUserHooks(t *testing.T) {
	settings := &claudeSettings{raw: map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo user"},
						map[string]any{"type": "command", "command": "old recoil", "marker": recoilHookMarker},
					},
				},
			},
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo start"},
					},
				},
			},
			"UserPromptSubmit": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "old recoil", "marker": recoilHookMarker},
					},
				},
			},
		},
	}}

	mergeClaudeHooks(settings)
	mergeClaudeHooks(settings)

	hooks := settings.raw["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; ok {
		t.Fatalf("expected old Recoil UserPromptSubmit hook to be removed, got %+v", hooks["UserPromptSubmit"])
	}
	if got := countClaudeHookMarkers(hooks["SessionStart"]); got != 1 {
		t.Fatalf("expected one Recoil SessionStart hook after repeated merge, got %d: %+v", got, hooks["SessionStart"])
	}
	if got := countClaudeHookCommands(hooks["PreToolUse"], "echo user"); got != 1 {
		t.Fatalf("expected user PreToolUse hook to survive marker cleanup, got %d: %+v", got, hooks["PreToolUse"])
	}

	removeClaudeHooks(settings)
	hooks = settings.raw["hooks"].(map[string]any)
	if got := countClaudeHookMarkers(hooks["SessionStart"]) + countClaudeHookMarkers(hooks["PreToolUse"]); got != 0 {
		t.Fatalf("expected Recoil hooks removed, got marker count %d: %+v", got, hooks)
	}
	if got := countClaudeHookCommands(hooks["SessionStart"], "echo start"); got != 1 {
		t.Fatalf("expected user SessionStart hook to survive uninstall, got %d: %+v", got, hooks["SessionStart"])
	}
	if got := countClaudeHookCommands(hooks["PreToolUse"], "echo user"); got != 1 {
		t.Fatalf("expected user PreToolUse hook to survive uninstall, got %d: %+v", got, hooks["PreToolUse"])
	}
	if settings.raw["theme"] != "dark" {
		t.Fatalf("expected unrelated setting to survive, got %+v", settings.raw)
	}
}

func TestOpenCodeInstallProjectScopeWritesManagedPlugin(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	path, summary, err := installOpenCodeHooks("project", false)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(".opencode", "plugins", opencodePluginName) {
		t.Fatalf("unexpected path %q", path)
	}
	if !strings.Contains(summary, "experimental.chat.system.transform") {
		t.Fatalf("expected plugin summary, got:\n%s", summary)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, opencodePluginPrefix) {
		t.Fatalf("expected managed plugin prefix, got:\n%s", got)
	}
	if !strings.Contains(got, "recoil hook remind --format=text") {
		t.Fatalf("expected remind command, got:\n%s", got)
	}

	if _, _, err := uninstallOpenCodeHooks("project", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected managed plugin removed, stat err=%v", err)
	}
}

func TestOpenCodeInstallRefusesForeignPlugin(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(".opencode", "plugins", opencodePluginName)
	writeCmdTestFile(t, path, "// user plugin\n")

	if _, _, err := installOpenCodeHooks("project", false); err == nil {
		t.Fatal("expected foreign OpenCode plugin refusal")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "// user plugin\n" {
		t.Fatalf("expected foreign plugin to remain untouched, got:\n%s", string(data))
	}
}

func TestCodexInstallProjectScopeManagesInstructionBlock(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeCmdTestFile(t, "AGENTS.md", "# Existing\n\nKeep this.\n")

	if _, _, err := installCodexHooks("project", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installCodexHooks("project", false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Count(got, codexManagedBlockOpen) != 1 {
		t.Fatalf("expected one managed block after repeated install, got:\n%s", got)
	}
	if !strings.Contains(got, "# Existing") || !strings.Contains(got, "recoil wake --max-chars 1600") {
		t.Fatalf("expected existing content and Recoil guidance, got:\n%s", got)
	}

	if _, _, err := uninstallCodexHooks("project", false); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	got = string(data)
	if strings.Contains(got, codexManagedBlockOpen) || strings.Contains(got, codexManagedBlockEnd) {
		t.Fatalf("expected managed block removed, got:\n%s", got)
	}
	if !strings.Contains(got, "# Existing") || !strings.Contains(got, "Keep this.") {
		t.Fatalf("expected existing content preserved, got:\n%s", got)
	}
}

func countClaudeHookMarkers(event any) int {
	count := 0
	groups, _ := event.([]any)
	for _, groupValue := range groups {
		group, _ := groupValue.(map[string]any)
		hooks, _ := group["hooks"].([]any)
		for _, hookValue := range hooks {
			hook, _ := hookValue.(map[string]any)
			if hook["marker"] == recoilHookMarker {
				count++
			}
		}
	}
	return count
}

func countClaudeHookCommands(event any, command string) int {
	count := 0
	groups, _ := event.([]any)
	for _, groupValue := range groups {
		group, _ := groupValue.(map[string]any)
		hooks, _ := group["hooks"].([]any)
		for _, hookValue := range hooks {
			hook, _ := hookValue.(map[string]any)
			if hook["command"] == command {
				count++
			}
		}
	}
	return count
}
