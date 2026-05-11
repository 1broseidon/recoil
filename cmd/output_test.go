package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSONUsesKindAndDataEnvelope(t *testing.T) {
	var b bytes.Buffer
	if err := writeJSON(&b, "search_result", []string{"mem_a"}); err != nil {
		t.Fatal(err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["version"]) != `"0.1"` {
		t.Fatalf("expected version 0.1, got %s", got["version"])
	}
	if string(got["kind"]) != `"search_result"` {
		t.Fatalf("expected kind search_result, got %s", got["kind"])
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("expected data field, got %s", b.String())
	}
	if _, ok := got["results"]; ok {
		t.Fatalf("did not expect legacy top-level results field, got %s", b.String())
	}
}

func TestFrontmatterQuotesUnsafeValues(t *testing.T) {
	var b bytes.Buffer
	err := frontmatter(&b, []kv{
		{k: "query", v: "why: auth"},
		{k: "empty", v: ""},
	}, "content")
	if err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, `query: "why: auth"`) {
		t.Fatalf("expected quoted query, got:\n%s", got)
	}
	if !strings.Contains(got, `empty: ""`) {
		t.Fatalf("expected quoted empty value, got:\n%s", got)
	}
	if !strings.HasSuffix(got, "content\n") {
		t.Fatalf("expected trailing newline, got:\n%s", got)
	}
}
