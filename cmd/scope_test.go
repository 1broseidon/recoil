package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/spf13/cobra"
)

func TestResolveScopeDefaultsToProject(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	sc, err := resolveScope(&cobra.Command{}, scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "project" {
		t.Fatalf("expected project scope, got %q", sc.Kind)
	}
	if !sc.Initialized {
		t.Fatal("expected initialized project scope")
	}
}

func TestResolveScopeHonorsUserFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	sc, err := resolveScope(&cobra.Command{}, scopeOptions{user: true})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "user" {
		t.Fatalf("expected user scope, got %q", sc.Kind)
	}
}

func TestResolveScopeRejectsMultipleExplicitScopes(t *testing.T) {
	_, err := resolveScope(&cobra.Command{}, scopeOptions{
		user:    true,
		session: "session-1",
	})
	if err == nil || !strings.Contains(err.Error(), "choose only one") {
		t.Fatalf("expected exclusive scope error, got %v", err)
	}
}

func TestResolveScopeHonorsSessionFlagWithoutProjectWarning(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errOut)
	sc, err := resolveScope(cmd, scopeOptions{session: " session-1 "})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "session" || sc.ID != "session-1" || sc.SessionID != "session-1" {
		t.Fatalf("unexpected session scope: %+v", sc)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no project warning for session scope, got %q", errOut.String())
	}
}

func TestResolveScopeWarnsForUninitializedDefaultProject(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errOut)
	sc, err := resolveScope(cmd, scopeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "project" || sc.Initialized {
		t.Fatalf("expected uninitialized project fallback, got %+v", sc)
	}
	got := errOut.String()
	if !strings.Contains(got, "current directory is not inside an initialized Recoil project") ||
		!strings.Contains(got, "run `recoil init` or pass --user/--session") {
		t.Fatalf("expected default project warning, got %q", got)
	}
}

func TestResolveScopeWarnsForUninitializedExplicitProject(t *testing.T) {
	root := t.TempDir()

	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errOut)
	sc, err := resolveScope(cmd, scopeOptions{project: root})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "project" || sc.Initialized {
		t.Fatalf("expected uninitialized explicit project fallback, got %+v", sc)
	}
	got := errOut.String()
	if !strings.Contains(got, "project path") ||
		!strings.Contains(got, "for a non-project memory") {
		t.Fatalf("expected explicit project warning, got %q", got)
	}
}

func TestCommandsIsolateDefaultProjectExplicitProjectUserAndSessionScopes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectA := t.TempDir()
	projectB := t.TempDir()
	if _, err := scope.InitProject(projectA); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.InitProject(projectB); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectA)

	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db")}
	defer func() { opts = oldOpts }()

	runScopeCommand(t, newAddCommand(), []string{"alpha project lane memory"})
	runScopeCommand(t, newAddCommand(), []string{"--project", projectB, "beta explicit project lane memory"})
	runScopeCommand(t, newAddCommand(), []string{"--user", "user lane memory"})
	runScopeCommand(t, newAddCommand(), []string{"--session", "session-a", "session lane memory"})

	defaultProject := runScopeCommand(t, newListCommand(), []string{"--minimal"})
	assertContainsOnly(t, defaultProject, "alpha project lane memory", []string{
		"beta explicit project lane memory",
		"user lane memory",
		"session lane memory",
	})

	explicitProject := runScopeCommand(t, newListCommand(), []string{"--project", projectB, "--minimal"})
	assertContainsOnly(t, explicitProject, "beta explicit project lane memory", []string{
		"alpha project lane memory",
		"user lane memory",
		"session lane memory",
	})

	user := runScopeCommand(t, newListCommand(), []string{"--user", "--minimal"})
	assertContainsOnly(t, user, "user lane memory", []string{
		"alpha project lane memory",
		"beta explicit project lane memory",
		"session lane memory",
	})

	session := runScopeCommand(t, newListCommand(), []string{"--session", "session-a", "--minimal"})
	assertContainsOnly(t, session, "session lane memory", []string{
		"alpha project lane memory",
		"beta explicit project lane memory",
		"user lane memory",
	})
}

func runScopeCommand(t *testing.T, c *cobra.Command, args []string) string {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errOut)
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatalf("%s failed: %v\nstderr:\n%s\nstdout:\n%s", c.Name(), err, errOut.String(), out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("%s wrote unexpected stderr:\n%s", c.Name(), errOut.String())
	}
	return out.String()
}

func assertContainsOnly(t *testing.T, got, want string, forbidden []string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in output:\n%s", want, got)
	}
	for _, item := range forbidden {
		if strings.Contains(got, item) {
			t.Fatalf("did not expect %q in output:\n%s", item, got)
		}
	}
}
