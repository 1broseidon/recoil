package cmd

import (
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
)

func TestOpenStoreFallsBackToProjectDBWhenDefaultDBIsInaccessible(t *testing.T) {
	oldOpts := opts
	opts = globalOptions{}
	defer func() { opts = oldOpts }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCmdTestFile(t, filepath.Join(home, "Library"), "not a directory")

	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	st, dbPath, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sc, err := scope.ProjectScope(".")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sc.Root, scope.ProjectDirName, "recoil.db")
	if dbPath != want {
		t.Fatalf("expected project-local fallback DB %q, got %q", want, dbPath)
	}
}

func TestOpenStoreDoesNotFallbackWhenDBPathExplicit(t *testing.T) {
	oldOpts := opts
	defer func() { opts = oldOpts }()

	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "missing", "child", "recoil.db")}
	st, dbPath, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if dbPath == filepath.Join(root, scope.ProjectDirName, "recoil.db") {
		t.Fatalf("did not expect explicit DB path to use project fallback")
	}
}
