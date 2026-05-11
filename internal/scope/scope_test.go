package scope

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitProjectCreatesMarkerAndNestedProjectScopeFindsIt(t *testing.T) {
	root := t.TempDir()
	sc, err := InitProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Initialized {
		t.Fatal("expected initialized project scope")
	}
	if sc.MarkerPath == "" {
		t.Fatal("expected marker path")
	}
	if _, err := os.Stat(filepath.Join(root, ProjectDirName, ProjectFileName)); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedScope, err := ProjectScope(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !nestedScope.Initialized {
		t.Fatal("expected nested scope to find initialized project")
	}
	if nestedScope.Root != sc.Root {
		t.Fatalf("expected root %q, got %q", sc.Root, nestedScope.Root)
	}
	if nestedScope.ProjectID != sc.ProjectID {
		t.Fatalf("expected project id %q, got %q", sc.ProjectID, nestedScope.ProjectID)
	}
}

func TestProjectScopeWithoutMarkerUsesUninitializedFallback(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Initialized {
		t.Fatal("expected uninitialized project fallback")
	}
	if sc.Root != realRoot {
		t.Fatalf("expected root %q, got %q", realRoot, sc.Root)
	}
	if sc.ProjectID == "" {
		t.Fatal("expected fallback project id")
	}
}
