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
	file, err := ReadFile(result.Path, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Records) != 1 || strings.Contains(file.Records[0].Content, "noisy log") {
		t.Fatalf("expected compact evidence without tool payload, got %+v", file.Records)
	}
}
