package scope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testGitEnv is gitEnv (the process environment minus the variables git
// exports to hooks) plus a fixed author, so commits in a temp repository do not
// depend on the developer's git config. Without the scrub a child git running
// in a temp repository would read this repository's index, which is how these
// tests failed under .githooks/pre-commit.
func testGitEnv() []string {
	return append(gitEnv(),
		"GIT_AUTHOR_NAME=recoil-test", "GIT_AUTHOR_EMAIL=recoil@test.invalid",
		"GIT_COMMITTER_NAME=recoil-test", "GIT_COMMITTER_EMAIL=recoil@test.invalid",
	)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// tempRepoWithWorktree builds a real repository with one real linked worktree.
func tempRepoWithWorktree(t *testing.T) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	repo = filepath.Join(base, "repo")
	initCmd := exec.Command("git", "init", "-q", repo)
	initCmd.Env = gitEnv()
	if err := initCmd.Run(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-q", "-m", "seed")

	worktree = filepath.Join(base, "wt")
	runGit(t, repo, "worktree", "add", "-q", worktree, "-b", "wt-branch")

	canonicalRepo, err := canonicalDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	return canonicalRepo, worktree
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectScopeResolvesWorktreeToMainRepository(t *testing.T) {
	repo, worktree := tempRepoWithWorktree(t)

	repoScope, err := ProjectScope(repo)
	if err != nil {
		t.Fatal(err)
	}
	worktreeScope, err := ProjectScope(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if worktreeScope.ID != repoScope.ID {
		t.Fatalf("worktree scope %q must match repository scope %q", worktreeScope.ID, repoScope.ID)
	}
	if worktreeScope.Root != repoScope.Root {
		t.Fatalf("expected the worktree to resolve Root to the main repository %q, got %q", repoScope.Root, worktreeScope.Root)
	}
	if worktreeScope.Unknown || repoScope.Unknown {
		t.Fatal("a git repository is never an unknown scope")
	}
}

func TestProjectScopeWorktreeInheritsTheRepositoryMarker(t *testing.T) {
	repo, worktree := tempRepoWithWorktree(t)

	initialized, err := InitProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.Initialized {
		t.Fatal("expected InitProject to write a marker")
	}

	worktreeScope, err := ProjectScope(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if !worktreeScope.Initialized {
		t.Fatal("expected the worktree to see the main repository's marker")
	}
	if worktreeScope.ID != initialized.ID {
		t.Fatalf("expected marker-derived id %q from the worktree, got %q", initialized.ID, worktreeScope.ID)
	}
	if worktreeScope.MarkerPath != initialized.MarkerPath {
		t.Fatalf("expected marker path %q, got %q", initialized.MarkerPath, worktreeScope.MarkerPath)
	}
}

func TestProjectScopeSubdirectoryStillResolvesToRepositoryRoot(t *testing.T) {
	repo, _ := tempRepoWithWorktree(t)
	sub := filepath.Join(repo, "nested", "deeper")
	writeFile(t, filepath.Join(sub, "keep.txt"), "x\n")

	subScope, err := ProjectScope(sub)
	if err != nil {
		t.Fatal(err)
	}
	repoScope, err := ProjectScope(repo)
	if err != nil {
		t.Fatal(err)
	}
	if subScope.ID != repoScope.ID || subScope.Root != repoScope.Root {
		t.Fatalf("a subdirectory must resolve to the repository root: %+v vs %+v", subScope, repoScope)
	}
}

// A git hook exports GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE. recoil run from
// inside one must still resolve a nested directory to its own repository root.
func TestProjectScopeIgnoresGitHookEnvironment(t *testing.T) {
	repo, _ := tempRepoWithWorktree(t)
	sub := filepath.Join(repo, "nested", "deeper")
	writeFile(t, filepath.Join(sub, "keep.txt"), "x\n")

	t.Setenv("GIT_DIR", ".git")
	t.Setenv("GIT_WORK_TREE", ".")
	t.Setenv("GIT_INDEX_FILE", ".git/index")

	subScope, err := ProjectScope(sub)
	if err != nil {
		t.Fatal(err)
	}
	if subScope.Unknown {
		t.Fatalf("a nested directory must be recognised as part of the repository: %+v", subScope)
	}
	if subScope.Root != repo {
		t.Fatalf("a nested directory must resolve to the repository root under a hook's environment: got %s, want %s", subScope.Root, repo)
	}
}

func TestProjectScopeMarksUnknownOutsideAnyRepository(t *testing.T) {
	bare := t.TempDir()

	sc, err := ProjectScope(bare)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Unknown {
		t.Fatalf("expected a directory with no repository and no marker to be Unknown, got %+v", sc)
	}
	if sc.Initialized || sc.Portable {
		t.Fatalf("an unknown scope is neither initialized nor portable: %+v", sc)
	}

	if _, err := InitProject(bare); err != nil {
		t.Fatal(err)
	}
	initialized, err := ProjectScope(bare)
	if err != nil {
		t.Fatal(err)
	}
	if initialized.Unknown {
		t.Fatal("a marker makes the scope addressable, so Unknown must clear")
	}
}
