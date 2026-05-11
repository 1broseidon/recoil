package cmd

import (
	"bytes"
	"strings"
	"testing"
)

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
