package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/store"
)

type sessionEvidenceMeta struct {
	NativeSessionID string   `json:"native_session_id"`
	Branch          string   `json:"branch"`
	EvidenceType    string   `json:"evidence_type"`
	TurnStart       int      `json:"turn_start"`
	TurnEnd         int      `json:"turn_end"`
	TouchedPaths    []string `json:"touched_paths"`
	DerivedType     string   `json:"derived_type"`
}

func parseSessionEvidenceMeta(mem store.Memory) sessionEvidenceMeta {
	var meta sessionEvidenceMeta
	if strings.TrimSpace(mem.MetadataJSON) == "" {
		return meta
	}
	_ = json.Unmarshal([]byte(mem.MetadataJSON), &meta)
	return meta
}

func sessionEvidenceProvenanceLines(mem store.Memory) []string {
	if !isSessionEvidence(mem) {
		return nil
	}
	meta := parseSessionEvidenceMeta(mem)
	var lines []string
	if mem.SourceAgent != "" {
		lines = append(lines, "agent: "+mem.SourceAgent)
	}
	if mem.SessionID != "" {
		lines = append(lines, "session_id: "+mem.SessionID)
		short := shortSessionID(mem.SessionID)
		if short != "" && short != mem.SessionID {
			lines = append(lines, "session_short_id: "+short)
		}
	}
	if meta.NativeSessionID != "" && meta.NativeSessionID != mem.SessionID {
		lines = append(lines, "native_session_id: "+meta.NativeSessionID)
		short := shortSessionID(meta.NativeSessionID)
		if short != "" && short != meta.NativeSessionID {
			lines = append(lines, "native_session_short_id: "+short)
		}
	}
	if meta.Branch != "" {
		lines = append(lines, "branch: "+meta.Branch)
	}
	if mem.CreatedAt != "" {
		lines = append(lines, "timestamp: "+mem.CreatedAt)
	}
	if meta.TurnStart > 0 || meta.TurnEnd > 0 {
		lines = append(lines, fmt.Sprintf("turn_range: %d-%d", meta.TurnStart, meta.TurnEnd))
	}
	return lines
}

func sessionEvidenceProvenanceKVs(mem store.Memory) []kv {
	var out []kv
	for _, line := range sessionEvidenceProvenanceLines(mem) {
		key, value, ok := strings.Cut(line, ": ")
		if ok {
			out = append(out, kv{k: key, v: value})
		}
	}
	if ref := sessionEvidenceSourceRef(mem); ref != "" {
		out = append(out, kv{k: "source_ref", v: ref})
	}
	return out
}

func sessionEvidenceSourceRef(mem store.Memory) string {
	if !isSessionEvidence(mem) {
		return mem.SourceRef
	}
	if strings.TrimSpace(mem.SourceRef) != "" {
		return strings.TrimSpace(mem.SourceRef)
	}
	meta := parseSessionEvidenceMeta(mem)
	if meta.TurnStart > 0 || meta.TurnEnd > 0 {
		return fmt.Sprintf("turn %d-%d", meta.TurnStart, meta.TurnEnd)
	}
	return ""
}

func sessionEvidenceDisplaySource(mem store.Memory) string {
	if !isSessionEvidence(mem) {
		return firstNonEmpty(mem.SourceAgent, mem.SourcePath)
	}
	parts := []string{"session_evidence"}
	if mem.SourceAgent != "" {
		parts = append(parts, mem.SourceAgent)
	}
	if mem.SessionID != "" {
		parts = append(parts, shortSessionID(mem.SessionID))
	}
	meta := parseSessionEvidenceMeta(mem)
	if meta.Branch != "" {
		parts = append(parts, meta.Branch)
	}
	if ref := sessionEvidenceSourceRef(mem); ref != "" {
		parts = append(parts, ref)
	}
	return strings.Join(parts, ":")
}

func shortSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if len(sessionID) <= 12 {
		return sessionID
	}
	return sessionID[:12]
}
