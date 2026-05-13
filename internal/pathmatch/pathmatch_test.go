package pathmatch

import "testing"

func TestMatchSupportsRecursiveGlobSegments(t *testing.T) {
	tests := []struct {
		pattern string
		rel     string
		want    bool
	}{
		{"docs/runbooks/**", "docs/runbooks/deploy.yaml", true},
		{"docs/**/deploy.yaml", "docs/platform/runbooks/deploy.yaml", true},
		{"docs/**/*.md", "docs/a/b/c/readme.md", true},
		{"docs/**/*.md", "src/readme.md", false},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", false},
	}
	for _, tc := range tests {
		if got := Match(tc.pattern, tc.rel); got != tc.want {
			t.Fatalf("Match(%q, %q) = %t, want %t", tc.pattern, tc.rel, got, tc.want)
		}
	}
}
