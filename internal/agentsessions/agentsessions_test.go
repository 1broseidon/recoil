package agentsessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeLines writes JSONL fixture content to path, creating parent dirs.
func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeDiscoverFiltersByCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	foreign := t.TempDir()

	// In-repo session and a foreign one share an encoding-agnostic layout;
	// the cwd recorded inside each file is the only authority.
	writeLines(t, filepath.Join(home, ".claude", "projects", "enc-a", "mine.jsonl"),
		`{"type":"user","timestamp":"2026-05-01T00:00:00Z","cwd":"`+repo+`","sessionId":"s-mine","message":{"role":"user","content":"We should use net/http, do not add a third-party client."}}`,
		`{"type":"assistant","timestamp":"2026-05-01T00:01:00Z","cwd":"`+repo+`","message":{"role":"assistant","content":[{"type":"text","text":"Done. Next step: add timeout tests around the gateway."},{"type":"tool_use","name":"Edit"}]}}`,
	)
	writeLines(t, filepath.Join(home, ".claude", "projects", "enc-b", "other.jsonl"),
		`{"type":"user","timestamp":"2026-05-01T00:00:00Z","cwd":"`+foreign+`","sessionId":"s-other","message":{"role":"user","content":"Switch this other repo to Redis for caching."}}`,
	)

	refs, err := claudeAdapter{}.Discover(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected exactly the in-repo session, got %d: %+v", len(refs), refs)
	}
	if refs[0].SessionID != "s-mine" {
		t.Fatalf("discovered wrong session: %q", refs[0].SessionID)
	}

	turns, err := claudeAdapter{}.Read(refs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d: %+v", len(turns), turns)
	}
	// tool_use block dropped, text kept, original timestamp preserved.
	if got := turns[1].Content; got == "" || got == "Edit" {
		t.Fatalf("assistant content not extracted from text block: %q", got)
	}
	if turns[0].Timestamp != "2026-05-01T00:00:00Z" {
		t.Fatalf("original timestamp not preserved: %q", turns[0].Timestamp)
	}
}

func TestCodexDiscoverFiltersByCwdAndReads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	foreign := t.TempDir()

	writeLines(t, filepath.Join(home, ".codex", "sessions", "2026", "05", "14", "rollout-mine.jsonl"),
		`{"type":"session_meta","timestamp":"2026-05-14T00:00:00Z","payload":{"id":"c-mine","cwd":"`+repo+`","git":{"branch":"main"}}}`,
		`{"type":"response_item","timestamp":"2026-05-14T00:01:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Decided: keep mattn/go-sqlite3 for FTS5; do not switch drivers."}]}}`,
		`{"type":"response_item","timestamp":"2026-05-14T00:01:30Z","payload":{"type":"reasoning","content":"ignored"}}`,
	)
	writeLines(t, filepath.Join(home, ".codex", "sessions", "2026", "05", "14", "rollout-other.jsonl"),
		`{"type":"session_meta","timestamp":"2026-05-14T00:00:00Z","payload":{"id":"c-other","cwd":"`+foreign+`"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"unrelated repo work"}]}}`,
	)

	refs, err := codexAdapter{}.Discover(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].SessionID != "c-mine" {
		t.Fatalf("codex discovery did not filter by cwd: %+v", refs)
	}
	if refs[0].Branch != "main" {
		t.Fatalf("expected branch from session_meta.git, got %q", refs[0].Branch)
	}

	turns, err := codexAdapter{}.Read(refs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 message turn (reasoning dropped), got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || turns[0].Timestamp != "2026-05-14T00:01:00Z" {
		t.Fatalf("unexpected turn: %+v", turns[0])
	}
}

func TestCursorsSkipUnchanged(t *testing.T) {
	state := t.TempDir()
	cursors, err := LoadCursors(state, "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	ref := SessionRef{Agent: "codex", SessionID: "display-session", NativeSessionID: "native-session", Path: "/x/rollout.jsonl", ModTime: time.Unix(1_700_000_000, 0).UTC(), Size: 4096}
	if cursors.Unchanged(ref) {
		t.Fatal("fresh cursor store should not report unchanged")
	}
	cursors.Record(ref, 3, time.Unix(1_700_000_100, 0).UTC())
	if !cursors.Unchanged(ref) {
		t.Fatal("recorded ref should be unchanged on identical mod-time/size")
	}
	if !cursors.SeenNative(ref) {
		t.Fatal("recorded native session should be marked seen")
	}
	moved := ref
	moved.Path = "/elsewhere/rollout-copy.jsonl"
	if !cursors.Unchanged(moved) {
		t.Fatal("cursor should key unchanged sessions by native id, not path")
	}
	grown := ref
	grown.Size = 8192
	if cursors.Unchanged(grown) {
		t.Fatal("a grown (appended) session must not be treated as unchanged")
	}
	if err := cursors.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadCursors(state, "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Unchanged(ref) {
		t.Fatal("cursor state did not persist across reload")
	}
}
