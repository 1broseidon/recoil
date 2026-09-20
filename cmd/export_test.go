package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

// longDoctrineBody is comfortably longer than wake's default --max-chars budget
// so the truncation contrast is unambiguous.
func longDoctrineBody(marker string) string {
	return marker + ": " + strings.Repeat("doctrine sentence that must survive export verbatim. ", 120)
}

func exportFixture(t *testing.T, jsonOut bool) scope.Scope {
	t.Helper()
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: jsonOut}
	t.Cleanup(func() { opts = oldOpts })

	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	rows := []struct {
		key      string
		content  string
		validity string
		created  string
	}{
		{"voice.pillars", longDoctrineBody("pillars-current"), "active", "2026-07-20T10:00:00Z"},
		{"voice.pillars", longDoctrineBody("pillars-old"), "superseded", "2026-07-10T10:00:00Z"},
		{"voice.bans", longDoctrineBody("bans-current"), "active", "2026-07-21T10:00:00Z"},
		{"voice_x", longDoctrineBody("underscore-decoy"), "active", "2026-07-22T10:00:00Z"},
		{"hotline.channel", longDoctrineBody("unrelated"), "active", "2026-07-23T10:00:00Z"},
	}
	for _, row := range rows {
		if _, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:      "decision",
			Content:   row.content,
			ScopeKind: sc.Kind,
			ScopeID:   sc.ID,
			ClaimKey:  row.key,
			Validity:  row.validity,
			CreatedAt: row.created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return sc
}

func runExport(t *testing.T, args ...string) string {
	t.Helper()
	c := newExportCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func exportJSON(t *testing.T, args ...string) exportResult {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(runExport(t, args...)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != "export_result" {
		t.Fatalf("expected export_result envelope, got %q", env.Kind)
	}
	data, _ := json.Marshal(env.Data)
	var result exportResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExportRequiresAClaimFamily(t *testing.T) {
	exportFixture(t, true)

	c := newExportCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected export without a claim family to fail")
	}
	code, exitCode := classifyError(err)
	if code != "VALIDATION" || exitCode != exitValidation {
		t.Fatalf("expected VALIDATION/%d, got %s/%d for %v", exitValidation, code, exitCode, err)
	}
}

func TestExportRejectsClaimKeyAndPrefixTogether(t *testing.T) {
	exportFixture(t, true)

	c := newExportCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key", "voice.pillars", "--claim-key-prefix", "voice."})
	if err := c.Execute(); err == nil {
		t.Fatal("expected mutually exclusive claim selectors to fail")
	}
}

func TestExportDefaultsToCurrentOnlyAndStableOrder(t *testing.T) {
	exportFixture(t, true)

	result := exportJSON(t, "--claim-key-prefix", "voice.")
	if result.Lifecycle != store.LifecycleCurrent {
		t.Fatalf("expected --current to default on, got lifecycle %q", result.Lifecycle)
	}
	if result.Count != 2 || len(result.Memories) != 2 {
		t.Fatalf("expected 2 current voice. memories, got %d: %+v", result.Count, result.Memories)
	}
	// claim_key ASC, so bans sorts before pillars regardless of created_at.
	if result.Memories[0].ClaimKey != "voice.bans" || result.Memories[1].ClaimKey != "voice.pillars" {
		t.Fatalf("expected claim_key ASC order, got %q then %q", result.Memories[0].ClaimKey, result.Memories[1].ClaimKey)
	}
	for _, mem := range result.Memories {
		if strings.Contains(mem.Content, "underscore-decoy") {
			t.Fatal("prefix voice. must not pull in voice_x")
		}
	}
}

func TestExportHistoricalIncludesPastWithinKeyNewestFirst(t *testing.T) {
	exportFixture(t, true)

	all := exportJSON(t, "--claim-key", "voice.pillars", "--current=false")
	if all.Lifecycle != store.LifecycleAny {
		t.Fatalf("expected --current=false to widen to any lifecycle, got %q", all.Lifecycle)
	}
	if len(all.Memories) != 2 {
		t.Fatalf("expected both pillars memories, got %d", len(all.Memories))
	}
	// created_at DESC within a key.
	if !strings.Contains(all.Memories[0].Content, "pillars-current") {
		t.Fatalf("expected the newest memory first, got %q", all.Memories[0].CreatedAt)
	}
	if !strings.Contains(all.Memories[1].Content, "pillars-old") {
		t.Fatalf("expected the older memory second, got %q", all.Memories[1].CreatedAt)
	}

	historical := exportJSON(t, "--claim-key", "voice.pillars", "--historical")
	if historical.Lifecycle != store.LifecycleHistorical {
		t.Fatalf("expected historical lifecycle, got %q", historical.Lifecycle)
	}
	if len(historical.Memories) != 1 || !strings.Contains(historical.Memories[0].Content, "pillars-old") {
		t.Fatalf("expected only the superseded memory, got %+v", historical.Memories)
	}
}

func TestExportEmptyResultIsNotAnError(t *testing.T) {
	exportFixture(t, true)

	result := exportJSON(t, "--claim-key-prefix", "nothing.")
	if result.Count != 0 || len(result.Memories) != 0 {
		t.Fatalf("expected an empty export, got %+v", result)
	}
}

func TestExportIsByteForByteDeterministic(t *testing.T) {
	exportFixture(t, false)

	first := runExport(t, "--claim-key-prefix", "voice.")
	second := runExport(t, "--claim-key-prefix", "voice.")
	if first != second {
		t.Fatalf("export text output is not deterministic:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	opts.json = true
	firstJSON := runExport(t, "--claim-key-prefix", "voice.")
	secondJSON := runExport(t, "--claim-key-prefix", "voice.")
	if firstJSON != secondJSON {
		t.Fatalf("export JSON output is not deterministic:\n--- first ---\n%s\n--- second ---\n%s", firstJSON, secondJSON)
	}
}

func TestExportTextEmitsFullBodiesAndMinimalFrontmatter(t *testing.T) {
	exportFixture(t, false)

	out := runExport(t, "--claim-key-prefix", "voice.")
	for _, want := range []string{
		"kind: export",
		"scope: project",
		"scope_id: ",
		"claim_key_prefix: voice.",
		"count: 2",
		"## voice.bans",
		"## voice.pillars",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in export output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "...") {
		t.Fatalf("export must not truncate; found an ellipsis:\n%s", out)
	}
	// Each full body appears verbatim, ellipsis-free.
	for _, marker := range []string{"pillars-current", "bans-current"} {
		if !strings.Contains(out, longDoctrineBody(marker)) {
			t.Fatalf("expected the full body for %q to survive export", marker)
		}
	}
}

// TestExportKeepsFullBodyWhereWakeTruncates is the reason export exists: wake's
// character budget silently clips doctrine, export never does.
func TestExportKeepsFullBodyWhereWakeTruncates(t *testing.T) {
	exportFixture(t, false)
	full := longDoctrineBody("pillars-current")

	exported := runExport(t, "--claim-key", "voice.pillars")
	if !strings.Contains(exported, full) {
		t.Fatal("export dropped part of the body")
	}

	w := newWakeCommand()
	var wakeOut bytes.Buffer
	w.SetOut(&wakeOut)
	w.SetErr(&bytes.Buffer{})
	w.SetArgs([]string{"--claim-key", "voice.pillars", "--max-chars", "400"})
	if err := w.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wakeOut.String(), full) {
		t.Fatalf("expected wake --max-chars 400 to truncate the body; it did not:\n%s", wakeOut.String())
	}
	if len(exported) <= len(wakeOut.String()) {
		t.Fatalf("expected the untruncated export (%d bytes) to exceed truncated wake (%d bytes)", len(exported), len(wakeOut.String()))
	}
}

func TestMCPExportReturnsFullFamilies(t *testing.T) {
	exportFixture(t, true)

	result, text, err := mcpExport(t.Context(), mcpExportInput{ClaimKeyPrefix: "voice."})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 {
		t.Fatalf("expected 2 current voice. memories, got %d", result.Count)
	}
	if !strings.Contains(text, longDoctrineBody("pillars-current")) {
		t.Fatal("expected the MCP export text to carry the full body")
	}
	if _, _, err := mcpExport(t.Context(), mcpExportInput{}); err == nil {
		t.Fatal("expected recoil_export to require a claim family")
	}
}
