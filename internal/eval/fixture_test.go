package eval

import (
	"strings"
	"testing"
)

func TestParseFixture(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"memory","id":"mem_one","scope":{"kind":"project","id":"project-recoil"},"role":"decision","content":"Use ordinary docs as memory sources.","metadata":{"validity":"active"}}`,
		`{"type":"case","id":"case_one","mode":"search","query":"docs memory","scope":{"kind":"project","id":"project-recoil"},"limit":5,"expected_current_ids":["mem_one"]}`,
	}, "\n")
	fixture, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Memories) != 1 || fixture.Memories[0].ID != "mem_one" {
		t.Fatalf("unexpected memories: %+v", fixture.Memories)
	}
	if len(fixture.Cases) != 1 || fixture.Cases[0].ExpectedCurrentIDs[0] != "mem_one" {
		t.Fatalf("unexpected cases: %+v", fixture.Cases)
	}
}

func TestParseFixtureRejectsUnknownType(t *testing.T) {
	_, err := Parse(strings.NewReader(`{"type":"surprise","id":"x"}`))
	if err == nil || !strings.Contains(err.Error(), `unknown type "surprise"`) {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestParseFixtureRejectsDuplicateMemoryID(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"memory","id":"mem_one","scope":{"kind":"project","id":"project-recoil"},"content":"first"}`,
		`{"type":"memory","id":"mem_one","scope":{"kind":"project","id":"project-recoil"},"content":"second"}`,
	}, "\n")
	_, err := Parse(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), `duplicate id "mem_one"`) {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}
