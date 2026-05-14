package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalCommandRunsFixture(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "fixtures.jsonl")
	fixture := strings.Join([]string{
		`{"type":"memory","id":"mem_current","scope":{"kind":"project","id":"project-recoil"},"role":"decision","created_at_order":10,"content":"Recoil eval harness uses ordinary docs and direct memories."}`,
		`{"type":"case","id":"eval-current","mode":"search","query":"ordinary docs direct memories","scope":{"kind":"project","id":"project-recoil"},"limit":5,"expected_current_ids":["mem_current"]}`,
	}, "\n")
	if err := os.WriteFile(fixturePath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	oldOpts := opts
	opts = globalOptions{}
	defer func() { opts = oldOpts }()

	c := newEvalCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{fixturePath})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"seeded_memories: 1",
		"case_count: 1",
		"passed: 1",
		"failed: 0",
		"results: mem_current",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestEvalCommandCanRunSemanticRetrieval(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "embeddings.jsonl")
	fixture := strings.Join([]string{
		`{"type":"memory","id":"mem_no_daemon","scope":{"kind":"project","id":"project-recoil"},"role":"decision","created_at_order":10,"content":"Recoil v0 has no daemon, no hosted service, and no cloud sync.","metadata":{"validity":"active","claim_key":"v0.non-goals"}}`,
		`{"type":"case","id":"semantic-current","mode":"search","query":"always-on background remote synchronization","scope":{"kind":"project","id":"project-recoil"},"limit":3,"expected_current_ids":["mem_no_daemon"]}`,
	}, "\n")
	if err := os.WriteFile(fixturePath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	oldOpts := opts
	opts = globalOptions{}
	defer func() { opts = oldOpts }()

	c := newEvalCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{fixturePath, "--retrieval", "semantic"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"retrieval: semantic",
		"provider: local",
		"indexed_embeddings: 1",
		"passed: 1",
		"results: mem_no_daemon",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestEvalArtifactPathsTreatExistingDottedDirectoryAsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "eval.out")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath, markdownPath, err := evalArtifactPaths(dir, "workflow")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(jsonPath) != dir || filepath.Dir(markdownPath) != dir {
		t.Fatalf("expected artifacts under %q, got %q and %q", dir, jsonPath, markdownPath)
	}
	if filepath.Ext(jsonPath) != ".json" || filepath.Ext(markdownPath) != ".md" {
		t.Fatalf("unexpected artifact extensions: %q %q", jsonPath, markdownPath)
	}
}
