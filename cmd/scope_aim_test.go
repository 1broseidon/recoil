package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

// aimTestProject returns an initialized project root plus its scope id.
func aimTestProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	sc, err := scope.InitProject(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, sc.ID
}

func newScopeProbeCommand() *cobra.Command {
	c := &cobra.Command{Use: "probe"}
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	return c
}

func TestRecoilProjectEnvAimsScopeLikeTheFlag(t *testing.T) {
	aimed, aimedID := aimTestProject(t)
	elsewhere, elsewhereID := aimTestProject(t)
	if aimedID == elsewhereID {
		t.Fatal("fixture projects must have distinct scope ids")
	}
	t.Chdir(elsewhere)

	// No env, no flag: cwd inference.
	sc, err := resolveReadScope(newScopeProbeCommand(), scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != elsewhereID {
		t.Fatalf("expected cwd inference to give %q, got %q", elsewhereID, sc.ID)
	}

	// Env aims it, exactly as --project would.
	t.Setenv(ProjectEnvVar, aimed)
	sc, err = resolveReadScope(newScopeProbeCommand(), scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != aimedID {
		t.Fatalf("expected %s to aim scope at %q, got %q", ProjectEnvVar, aimedID, sc.ID)
	}
	flagScope, err := resolveReadScope(newScopeProbeCommand(), scopeOptions{project: aimed})
	if err != nil {
		t.Fatal(err)
	}
	if flagScope.ID != sc.ID || flagScope.Root != sc.Root {
		t.Fatalf("env aim must match the flag exactly: %+v vs %+v", sc, flagScope)
	}
}

func TestProjectFlagBeatsEnvWhichBeatsCwd(t *testing.T) {
	flagged, flaggedID := aimTestProject(t)
	envAimed, envID := aimTestProject(t)
	cwd, cwdID := aimTestProject(t)
	t.Chdir(cwd)
	t.Setenv(ProjectEnvVar, envAimed)

	sc, err := resolveReadScope(newScopeProbeCommand(), scopeOptions{project: flagged})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != flaggedID {
		t.Fatalf("--project must beat %s: expected %q, got %q", ProjectEnvVar, flaggedID, sc.ID)
	}

	sc, err = resolveReadScope(newScopeProbeCommand(), scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != envID {
		t.Fatalf("%s must beat cwd (%q): expected %q, got %q", ProjectEnvVar, cwdID, envID, sc.ID)
	}
}

func TestUserAndSessionFlagsIgnoreTheProjectEnvAim(t *testing.T) {
	aimed, _ := aimTestProject(t)
	cwd, _ := aimTestProject(t)
	t.Chdir(cwd)
	t.Setenv(ProjectEnvVar, aimed)

	sc, err := resolveReadScope(newScopeProbeCommand(), scopeOptions{user: true})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "user" {
		t.Fatalf("--user must win over %s, got kind %q", ProjectEnvVar, sc.Kind)
	}

	sc, err = resolveReadScope(newScopeProbeCommand(), scopeOptions{session: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "session" || sc.ID != "sess-1" {
		t.Fatalf("--session must win over %s, got %+v", ProjectEnvVar, sc)
	}
}

func TestMCPScopeHonoursTheProjectEnvAim(t *testing.T) {
	aimed, aimedID := aimTestProject(t)
	cwd, cwdID := aimTestProject(t)
	t.Chdir(cwd)

	sc, err := resolveMCPScope(scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != cwdID {
		t.Fatalf("expected the MCP default to infer from cwd, got %q", sc.ID)
	}

	t.Setenv(ProjectEnvVar, aimed)
	sc, err = resolveMCPScope(scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != aimedID {
		t.Fatalf("expected the MCP server to honour %s, got %q", ProjectEnvVar, sc.ID)
	}
}

func TestUnknownScopeWarnsLoudlyOnStderrOnly(t *testing.T) {
	// A plain temp dir: no git repository, no .recoil marker, nothing to anchor.
	t.Chdir(t.TempDir())

	c := &cobra.Command{Use: "probe"}
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)

	sc, err := resolveReadScope(c, scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Unknown {
		t.Fatalf("expected an unknown scope, got %+v", sc)
	}
	warning := errOut.String()
	if out.Len() != 0 {
		t.Fatalf("the warning must not touch stdout, got %q", out.String())
	}
	if strings.Count(strings.TrimRight(warning, "\n"), "\n") != 0 {
		t.Fatalf("expected exactly one warning line, got %q", warning)
	}
	for _, want := range []string{"warning:", sc.ID, "--project", ProjectEnvVar} {
		if !strings.Contains(warning, want) {
			t.Fatalf("expected the warning to mention %q, got %q", want, warning)
		}
	}
}

func TestAimedAndInitializedScopesDoNotWarnAboutUnknownScope(t *testing.T) {
	aimed, _ := aimTestProject(t)

	// Aimed at a real project from an unanchored cwd: no unknown-scope warning.
	t.Chdir(t.TempDir())
	t.Setenv(ProjectEnvVar, aimed)
	c := &cobra.Command{Use: "probe"}
	var errOut bytes.Buffer
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&errOut)
	if _, err := resolveReadScope(c, scopeOptions{}); err != nil {
		t.Fatal(err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("an aimed, initialized project must be silent, got %q", errOut.String())
	}
}

func TestUnknownScopeWarningDoesNotChangeCommandOutputOrExit(t *testing.T) {
	// list over an unknown scope: same stdout shape, exit 0, warning on stderr.
	t.Chdir(t.TempDir())
	oldOpts := opts
	opts = globalOptions{dbPath: t.TempDir() + "/recoil.db", json: true}
	defer func() { opts = oldOpts }()

	c := newListCommand()
	var out, errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	if err := c.Execute(); err != nil {
		t.Fatalf("an unknown scope must not fail the command: %v", err)
	}
	if !strings.Contains(errOut.String(), "warning:") {
		t.Fatalf("expected the unknown-scope warning on stderr, got %q", errOut.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Fatalf("expected an unchanged JSON envelope on stdout, got %q", out.String())
	}
}
