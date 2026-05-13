package mine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectSkipsHiddenAndToolingDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Recoil\n\nUseful docs.")
	writeFile(t, filepath.Join(root, "docs", "notes.txt"), "plain text notes")
	writeFile(t, filepath.Join(root, ".brainfile", "board", "task.md"), "brainfile task")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "README.md"), "dependency docs")
	writeFile(t, filepath.Join(root, "image.png"), "not really an image")

	result, err := Collect(Options{Path: root, SourceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, chunk := range result.Chunks {
		paths = append(paths, chunk.SourcePath)
	}
	got := strings.Join(paths, ",")
	if got != "README.md,docs/notes.txt" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestCollectHonorsRecoilignorePatternsAndSentinel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Project\n\nKeep this.")
	writeFile(t, filepath.Join(root, "docs", "keep.md"), "keep me")
	writeFile(t, filepath.Join(root, "eval", "corpora", "docs-heavy", "files", "adr.md"), "fictional adr")
	writeFile(t, filepath.Join(root, "eval", "corpora", "docs-heavy", "files", "runbook.md"), "fictional runbook")
	writeFile(t, filepath.Join(root, "vendor-notes", "third-party", "README.md"), "3p docs")
	writeFile(t, filepath.Join(root, "vendor-notes", ".recoilignore"), "*\n")
	writeFile(t, filepath.Join(root, ".recoilignore"), "# top-level\neval/corpora/\n")

	result, err := Collect(Options{Path: root, SourceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, chunk := range result.Chunks {
		paths = append(paths, chunk.SourcePath)
	}
	got := strings.Join(paths, ",")
	if got != "README.md,docs/keep.md" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestCollectIncludesOperationalHiddenSecurityDoc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".github", "SECURITY.md"), "# Security Policy\n\nReport vulnerabilities here.")
	writeFile(t, filepath.Join(root, ".github", "pull_request_template.md"), "ordinary hidden repo template")

	result, err := Collect(Options{Path: root, SourceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := chunkPaths(result.Chunks)
	if got != ".github/SECURITY.md" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestCollectCanDisableOperationalHiddenSecurityDoc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".github", "SECURITY.md"), "# Security Policy\n\nReport vulnerabilities here.")

	result, err := Collect(Options{
		Path:                     root,
		SourceRoot:               root,
		IncludeHiddenOperational: false,
		FollowRepoSymlinks:       true,
		PolicyConfigured:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkPaths(result.Chunks); got != "" {
		t.Fatalf("expected hidden security doc to be skipped, got %s", got)
	}
}

func TestCollectFollowsSafeTextSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "apps", "docs", "public", ".well-known", "security.txt")
	writeFile(t, target, "Contact: https://hackerone.com/example\n")
	if err := os.Symlink(filepath.ToSlash(filepath.Join("apps", "docs", "public", ".well-known", "security.txt")), filepath.Join(root, "SECURITY.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	result, err := Collect(Options{Path: root, SourceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := chunkPaths(result.Chunks)
	if got != "SECURITY.md,apps/docs/public/.well-known/security.txt" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestCollectCanDisableRepoSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "docs", "security.md")
	writeFile(t, target, "Security contact lives here.\n")
	if err := os.Symlink(filepath.ToSlash(filepath.Join("docs", "security.md")), filepath.Join(root, "SECURITY.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	result, err := Collect(Options{
		Path:                     root,
		SourceRoot:               root,
		IncludeHiddenOperational: true,
		FollowRepoSymlinks:       false,
		PolicyConfigured:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkPaths(result.Chunks); got != "docs/security.md" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestCollectHonorsConfiguredIncludeAndExcludePaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "runbooks", "deploy.yaml"), "deploy: carefully\n")
	writeFile(t, filepath.Join(root, "docs", "generated", "README.md"), "generated docs\n")

	result, err := Collect(Options{
		Path:                     root,
		SourceRoot:               root,
		IncludeHiddenOperational: true,
		FollowRepoSymlinks:       true,
		PolicyConfigured:         true,
		IncludePaths:             []string{"docs/runbooks/**"},
		ExcludePaths:             []string{"docs/generated/**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkPaths(result.Chunks); got != "docs/runbooks/deploy.yaml" {
		t.Fatalf("unexpected mined paths: %s", got)
	}
}

func TestChunkTextAddsStableLineRefs(t *testing.T) {
	text := strings.Join([]string{
		"# Heading",
		"",
		"First paragraph.",
		"Second paragraph.",
		"Third paragraph.",
	}, "\n")
	chunks := ChunkText("README.md", text, 35)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].SourceRef != "chunk 1 lines 1-3" {
		t.Fatalf("unexpected first source ref: %q", chunks[0].SourceRef)
	}
	if chunks[1].SourceRef != "chunk 2 lines 4-5" {
		t.Fatalf("unexpected second source ref: %q", chunks[1].SourceRef)
	}
}

func TestChunksForFileRejectsBinaryContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte{'o', 'k', 0, 'n', 'o'}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ChunksForFile(File{Path: path, Rel: "notes.txt"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary error, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func chunkPaths(chunks []Chunk) string {
	var paths []string
	for _, chunk := range chunks {
		paths = append(paths, chunk.SourcePath)
	}
	return strings.Join(paths, ",")
}
