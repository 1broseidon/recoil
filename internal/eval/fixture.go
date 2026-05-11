package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Scope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Fixture struct {
	Memories []MemoryRecord
	Cases    []CaseRecord
}

type MemoryRecord struct {
	ID             string            `json:"id"`
	Scope          Scope             `json:"scope"`
	Role           string            `json:"role,omitempty"`
	SourceAgent    string            `json:"source_agent,omitempty"`
	SourcePath     string            `json:"source_path,omitempty"`
	SourceRef      string            `json:"source_ref,omitempty"`
	CreatedAtOrder int               `json:"created_at_order,omitempty"`
	Content        string            `json:"content"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type CaseRecord struct {
	ID                    string   `json:"id"`
	Category              string   `json:"category,omitempty"`
	Mode                  string   `json:"mode"`
	Query                 string   `json:"query,omitempty"`
	Scope                 Scope    `json:"scope"`
	Role                  string   `json:"role,omitempty"`
	ClaimKey              string   `json:"claim_key,omitempty"`
	Validity              string   `json:"validity,omitempty"`
	Lifecycle             string   `json:"lifecycle,omitempty"`
	SourceAgent           string   `json:"source_agent,omitempty"`
	SourcePath            string   `json:"source_path,omitempty"`
	Limit                 int      `json:"limit,omitempty"`
	ExpectedCurrentIDs    []string `json:"expected_current_ids,omitempty"`
	ExpectedHistoricalIDs []string `json:"expected_historical_ids,omitempty"`
	ForbiddenCurrentIDs   []string `json:"forbidden_current_ids,omitempty"`
	ForbiddenIDs          []string `json:"forbidden_ids,omitempty"`
	ExpectedEmpty         bool     `json:"expected_empty,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
}

func Load(path string) (Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return Fixture{}, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) (Fixture, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var fixture Fixture
	memoryIDs := make(map[string]int)
	caseIDs := make(map[string]int)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &header); err != nil {
			return Fixture{}, fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch header.Type {
		case "memory":
			var rec MemoryRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return Fixture{}, fmt.Errorf("line %d memory: %w", lineNo, err)
			}
			if err := validateMemory(rec); err != nil {
				return Fixture{}, fmt.Errorf("line %d memory: %w", lineNo, err)
			}
			if first, ok := memoryIDs[rec.ID]; ok {
				return Fixture{}, fmt.Errorf("line %d memory: duplicate id %q first seen on line %d", lineNo, rec.ID, first)
			}
			memoryIDs[rec.ID] = lineNo
			fixture.Memories = append(fixture.Memories, rec)
		case "case":
			var rec CaseRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return Fixture{}, fmt.Errorf("line %d case: %w", lineNo, err)
			}
			if err := validateCase(rec); err != nil {
				return Fixture{}, fmt.Errorf("line %d case: %w", lineNo, err)
			}
			if first, ok := caseIDs[rec.ID]; ok {
				return Fixture{}, fmt.Errorf("line %d case: duplicate id %q first seen on line %d", lineNo, rec.ID, first)
			}
			caseIDs[rec.ID] = lineNo
			fixture.Cases = append(fixture.Cases, rec)
		default:
			return Fixture{}, fmt.Errorf("line %d: unknown type %q", lineNo, header.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

func validateMemory(rec MemoryRecord) error {
	if strings.TrimSpace(rec.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if err := validateScope(rec.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(rec.Content) == "" {
		return fmt.Errorf("content is required")
	}
	return nil
}

func validateCase(rec CaseRecord) error {
	if strings.TrimSpace(rec.ID) == "" {
		return fmt.Errorf("id is required")
	}
	switch rec.Mode {
	case "search", "wake", "list":
	default:
		return fmt.Errorf("unsupported mode %q", rec.Mode)
	}
	if err := validateScope(rec.Scope); err != nil {
		return err
	}
	return nil
}

func validateScope(sc Scope) error {
	switch sc.Kind {
	case "project", "user", "session":
	default:
		return fmt.Errorf("unsupported scope kind %q", sc.Kind)
	}
	if strings.TrimSpace(sc.ID) == "" {
		return fmt.Errorf("scope id is required")
	}
	return nil
}
