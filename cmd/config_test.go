package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/sourcequality"
)

func TestConfigCommandSetGetAndExplain(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	oldOpts := opts
	opts = globalOptions{}
	defer func() { opts = oldOpts }()

	runConfigCommand(t, "set", "mine.follow_repo_symlinks", "false")
	got := runConfigCommand(t, "get", "mine.follow_repo_symlinks")
	if strings.TrimSpace(got) != "false" {
		t.Fatalf("expected project override, got %q", got)
	}
	explain := runConfigCommand(t, "explain", "mine.include_hidden_operational")
	if !strings.Contains(explain, "allowlisted hidden operational files") || !strings.Contains(explain, "default: true") {
		t.Fatalf("expected config explanation, got:\n%s", explain)
	}
}

func TestEffectiveSourceQualityOptionsFromConfig(t *testing.T) {
	settings := config.Settings{Values: map[string]string{
		"classify.override.docs/runbooks/**": "operational",
		"rank.search.boost.operational":      "2.5",
		"rank.wake.penalty.fixture":          "1.25",
	}}
	opts := effectiveSourceQualityOptions(settings)
	if len(opts.ClassOverrides) != 1 || opts.ClassOverrides[0].Pattern != "docs/runbooks/**" {
		t.Fatalf("expected class override, got %+v", opts.ClassOverrides)
	}
	if opts.SearchBoosts[sourcequality.ClassOperational] != 2.5 {
		t.Fatalf("expected search boost, got %+v", opts.SearchBoosts)
	}
	if opts.WakePenalties[sourcequality.ClassFixture] != 1.25 {
		t.Fatalf("expected wake penalty, got %+v", opts.WakePenalties)
	}
}

func runConfigCommand(t *testing.T, args ...string) string {
	t.Helper()
	c := newConfigCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
