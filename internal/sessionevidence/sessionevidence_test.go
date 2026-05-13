package sessionevidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSelectCapturesUserDirectiveAndRedacts(t *testing.T) {
	payload := `{
		"session_id": "sess_a8f3",
		"turns": [
			{"turn_index": 42, "role": "user", "content": "skip refresh tokens and use bearer abc.def as the temporary auth path", "timestamp": "2026-05-11T12:00:00Z"},
			{"turn_index": 43, "role": "assistant", "content": "Understood. I will keep bearer auth for v0 and avoid refresh-token endpoints."}
		]
	}`
	turns, sessionID, err := ParseTurns([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "sess_a8f3" {
		t.Fatalf("unexpected session id %q", sessionID)
	}
	records := Select(turns, Options{
		ScopeKind:   "project",
		ScopeID:     "project-1",
		SourceAgent: "codex",
		SessionID:   sessionID,
		Now:         time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	})
	if len(records) != 1 {
		t.Fatalf("expected one evidence record, got %+v", records)
	}
	if records[0].EvidenceType != "user_directive" {
		t.Fatalf("expected user directive evidence, got %+v", records[0])
	}
	if strings.Contains(records[0].Content, "abc.def") || !strings.Contains(records[0].Content, "bearer [REDACTED]") {
		t.Fatalf("expected bearer token redaction, got %q", records[0].Content)
	}
}

func TestSelectSkipsAssistantSpeculationWithoutToolEvidence(t *testing.T) {
	turns := []Turn{
		{Index: 1, Role: "assistant", Content: "I might implement a refresh token endpoint later if it seems useful."},
	}
	records := Select(turns, Options{ScopeKind: "project", ScopeID: "project-1", SessionID: "s"})
	if len(records) != 0 {
		t.Fatalf("expected speculation to be skipped, got %+v", records)
	}
}

func TestSelectCapturesDurablePersonalFacts(t *testing.T) {
	turns := []Turn{
		{Index: 1, Role: "user", Content: "I had a follow-up appointment with Dr. Lee, the dermatologist, after a benign biopsy."},
		{Index: 2, Role: "assistant", Content: "That is useful health context."},
		{Index: 3, Role: "user", Content: "Dr. Smith prescribed antibiotics after the urinary tract infection."},
		{Index: 4, Role: "assistant", Content: "I will remember the separate doctor context."},
	}
	records := Select(turns, Options{ScopeKind: "project", ScopeID: "project-1", SessionID: "sess-facts"})
	if len(records) != 2 {
		t.Fatalf("expected two personal fact records, got %+v", records)
	}
	for _, record := range records {
		if record.EvidenceType != "personal_fact" {
			t.Fatalf("expected personal fact evidence, got %+v", record)
		}
	}
	if !strings.Contains(records[0].Content, "Dr. Lee") || !strings.Contains(records[1].Content, "Dr. Smith") {
		t.Fatalf("expected doctor facts in selected evidence, got %+v", records)
	}
}

func TestParseTurnsClaudeCodeJSONLShape(t *testing.T) {
	// Real Claude Code transcript lines nest role+content inside a "message"
	// object, with assistant content arriving as typed blocks. Evidence must
	// extract the text turns and drop thinking / tool_use / tool_result blocks
	// even though they appear inside the same message.
	data := []byte(`{"type":"user","message":{"role":"user","content":"go with bearer tokens for v0, skip refresh tokens"},"sessionId":"sess_cc","timestamp":"2026-05-12T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing options","signature":"sig-abc"},{"type":"text","text":"Understood. I will skip refresh tokens. tests passed in cmd/auth_test.go"}]},"sessionId":"sess_cc","timestamp":"2026-05-12T10:00:01Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"thinking-only turn","signature":"sig-def"}]},"sessionId":"sess_cc","timestamp":"2026-05-12T10:00:02Z"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"build output"}]},"sessionId":"sess_cc","timestamp":"2026-05-12T10:00:03Z"}
`)
	turns, _, err := ParseTurns(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 text turns (thinking-only + tool_result dropped), got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || !strings.Contains(turns[0].Content, "bearer tokens") {
		t.Fatalf("unexpected user turn: %+v", turns[0])
	}
	if turns[1].Role != "assistant" || !strings.Contains(turns[1].Content, "tests passed") {
		t.Fatalf("unexpected assistant turn: %+v", turns[1])
	}
	for _, leak := range []string{"signature", "sig-abc", "thinking", "weighing options", "tool_result", "build output"} {
		if strings.Contains(turns[1].Content, leak) {
			t.Fatalf("internal block leaked into evidence content (%q): %q", leak, turns[1].Content)
		}
	}
}

func TestIngestWritesCompactEvidenceOnly(t *testing.T) {
	payload := []map[string]any{
		{"turn_index": 1, "role": "user", "content": "go with CGO sqlite for FTS5 support"},
		{"turn_index": 2, "role": "assistant", "content": "I'll keep the mattn sqlite path."},
		{"turn_index": 3, "role": "tool", "content": strings.Repeat("noisy log ", 200)},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	result, err := Ingest(data, Options{
		StateDir:    stateDir,
		ScopeKind:   "project",
		ScopeID:     "project-1",
		SourceAgent: "codex",
		SessionID:   "sess-test",
		Now:         time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Path == "" || !strings.Contains(result.SourcePath, "session-evidence/") {
		t.Fatalf("unexpected ingest result %+v", result)
	}
	if strings.Contains(result.SourcePath, "sess_sess-test") {
		t.Fatalf("did not expect double sess_ prefix, got %q", result.SourcePath)
	}
	file, err := ReadFile(result.Path, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Records) != 1 || strings.Contains(file.Records[0].Content, "noisy log") {
		t.Fatalf("expected compact evidence without tool payload, got %+v", file.Records)
	}
}

func TestSelectDedupesSameSpanAcrossEvidenceTypes(t *testing.T) {
	turns := []Turn{
		{Index: 8, Role: "user", Content: "use the compact session evidence path and update README.md"},
		{Index: 9, Role: "assistant", Content: "Done. I updated README.md and tests passed."},
	}
	records := Select(turns, Options{ScopeKind: "project", ScopeID: "project-1", SessionID: "sess-dedupe", MinChars: 20})
	if len(records) != 1 {
		t.Fatalf("expected same turn span to dedupe to one record, got %+v", records)
	}
	if got := strings.Join(records[0].EvidenceTypes, ","); got != "user_directive,completion_summary" {
		t.Fatalf("expected merged evidence types, got %q in %+v", got, records[0])
	}
}
