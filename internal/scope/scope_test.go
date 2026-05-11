package scope

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/config"
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

func TestProjectScopeAcceptsFilePathInsideInitializedProject(t *testing.T) {
	root := t.TempDir()
	sc, err := InitProject(root)
	if err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "docs", "decision.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("scope decision"), 0o600); err != nil {
		t.Fatal(err)
	}

	fromFile, err := ProjectScope(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !fromFile.Initialized || fromFile.Root != sc.Root || fromFile.ProjectID != sc.ProjectID {
		t.Fatalf("expected file path to resolve to initialized project, got %+v want root=%q id=%q", fromFile, sc.Root, sc.ProjectID)
	}
}

func TestProjectScopeUsesMarkerProjectIDAndPortableFlag(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, ProjectDirName)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(projectDir, ProjectFileName)
	if err := os.WriteFile(marker, []byte(`{"version":"0.1","project_id":"git:portable-project","created_at":"2026-01-01T00:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sc, err := ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Initialized || sc.ID != "git:portable-project" || sc.ProjectID != "git:portable-project" || !sc.Portable {
		t.Fatalf("expected marker project ID and portable flag, got %+v", sc)
	}
	realMarker, err := filepath.EvalSymlinks(marker)
	if err != nil {
		t.Fatal(err)
	}
	if sc.MarkerPath != realMarker {
		t.Fatalf("expected marker path %q, got %q", realMarker, sc.MarkerPath)
	}
}

func TestProjectScopeFallbackIDsAreRootSpecific(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	scopeA, err := ProjectScope(rootA)
	if err != nil {
		t.Fatal(err)
	}
	scopeB, err := ProjectScope(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if scopeA.Initialized || scopeB.Initialized {
		t.Fatalf("expected uninitialized fallback scopes, got %+v %+v", scopeA, scopeB)
	}
	if scopeA.ID == scopeB.ID {
		t.Fatalf("expected root-specific fallback IDs, both were %q", scopeA.ID)
	}
}

func TestUserScopePersistsStableIDInConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	first, err := UserScope()
	if err != nil {
		t.Fatal(err)
	}
	second, err := UserScope()
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != "user" || first.ID == "" || !first.Portable {
		t.Fatalf("unexpected user scope: %+v", first)
	}
	if first.ID != second.ID {
		t.Fatalf("expected stable user ID, got %q then %q", first.ID, second.ID)
	}
	path, err := config.ResolveUserIDPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first.ID+"\n" {
		t.Fatalf("expected persisted user ID %q, got %q", first.ID+"\n", string(data))
	}
}

func TestSessionScopeTrimsAndRejectsEmptyIDs(t *testing.T) {
	sc, err := SessionScope(" session-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if sc.Kind != "session" || sc.ID != "session-1" || sc.SessionID != "session-1" || !sc.Portable {
		t.Fatalf("unexpected session scope: %+v", sc)
	}

	if _, err := SessionScope(" \t "); err == nil {
		t.Fatal("expected empty session ID error")
	}
}
