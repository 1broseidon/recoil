package cmd

import (
	"path/filepath"
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
