package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
)

func TestSetupBootstrapsInitMineAndNextCommand(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "README.md"), "# Project\n\nSetup bootstrap searchable memory.")
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	c := newSetupCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--no-hooks"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Kind string `json:"kind"`
		Data struct {
			Mine struct {
				Chunks int `json:"chunks"`
				Added  int `json:"added"`
			} `json:"mine"`
			NextCommand string `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != "setup_result" || envelope.Data.NextCommand != "recoil wake" {
		t.Fatalf("unexpected setup result: %+v\n%s", envelope, out.String())
	}
	if envelope.Data.Mine.Chunks != 1 || envelope.Data.Mine.Added != 1 {
		t.Fatalf("expected first mine to add one chunk, got %+v", envelope.Data.Mine)
	}
}

func TestWakeRefreshesChangedTrackedSource(t *testing.T) {
	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	writeCmdTestFile(t, readme, "# Project\n\nAlpha wake refresh source.")
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	mineCmd := newMineCommand()
	mineCmd.SetOut(&bytes.Buffer{})
	mineCmd.SetErr(&bytes.Buffer{})
	if err := mineCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	writeCmdTestFile(t, readme, "# Project\n\nBeta wake refresh source.")
	wakeCmd := newWakeCommand()
	var wakeOut bytes.Buffer
	wakeCmd.SetOut(&wakeOut)
	wakeCmd.SetErr(&bytes.Buffer{})
	wakeCmd.SetArgs([]string{"Beta wake refresh", "--limit", "4"})
	if err := wakeCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := wakeOut.String()
	if !strings.Contains(got, "refreshed_sources: 1") {
		t.Fatalf("expected wake to refresh changed source:\n%s", got)
	}
	if !strings.Contains(got, "Beta wake refresh source") {
		t.Fatalf("expected refreshed content in wake output:\n%s", got)
	}
}

func TestRememberInfersDecisionAndClaimKey(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	c := newRememberCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--agent", "codex", "Use structured retrieval lanes for wake and search."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data rememberResult `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Memory.Role != "decision" {
		t.Fatalf("expected decision role, got %+v", envelope.Data.Memory)
	}
	if !strings.HasPrefix(envelope.Data.Memory.ClaimKey, "decision.") {
		t.Fatalf("expected generated decision claim key, got %q", envelope.Data.Memory.ClaimKey)
	}
}

func TestHandoffWritesStructuredCloseout(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	c := newHandoffCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{
		"--agent", "codex",
		"--decision", "Ship setup first.",
		"--constraint", "Wake must stay non-daemon.",
		"--next-step", "Add doctor as P1.",
		"P0 closeout.",
	})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"role: handoff", "## Decisions Made", "Ship setup first.", "## Constraints Discovered", "## Next Steps"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in handoff output:\n%s", want, got)
		}
	}
}

func TestSearchJSONIncludesStructuredLanesAndWhy(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	defer func() { opts = oldOpts }()

	remember := newRememberCommand()
	remember.SetOut(&bytes.Buffer{})
	remember.SetErr(&bytes.Buffer{})
	remember.SetArgs([]string{"Use channel-aware retrieval lanes for remote artifacts."})
	if err := remember.Execute(); err != nil {
		t.Fatal(err)
	}

	search := newSearchCommand()
	var out bytes.Buffer
	search.SetOut(&out)
	search.SetErr(&bytes.Buffer{})
	search.SetArgs([]string{"channel-aware retrieval lanes"})
	if err := search.Execute(); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Lanes []retrievalLaneResult `json:"lanes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Lanes) == 0 || envelope.Data.Lanes[0].Title != "Current Decisions" {
		t.Fatalf("expected Current Decisions lane, got %+v\n%s", envelope.Data.Lanes, out.String())
	}
	if got := envelope.Data.Lanes[0].Results[0].Why; got == "" {
		t.Fatalf("expected why metadata in structured lane: %+v", envelope.Data.Lanes[0].Results[0])
	}
}
