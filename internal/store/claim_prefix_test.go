package store

import (
	"context"
	"path/filepath"
	"testing"
)

func addClaim(t *testing.T, st *Store, scopeID, claimKey, content string) Memory {
	t.Helper()
	mem, _, err := st.AddMemory(context.Background(), AddMemoryParams{
		Role:      "decision",
		Content:   content,
		ScopeKind: "project",
		ScopeID:   scopeID,
		ClaimKey:  claimKey,
		Validity:  "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	return *mem
}

func claimPrefixFixture(t *testing.T) (*Store, string) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	const scopeID = "proj-prefix"
	addClaim(t, st, scopeID, "voice.pillars", "Speak plainly.")
	addClaim(t, st, scopeID, "voice.bubbles", "One thought per bubble.")
	// voice_x must NOT be matched by the prefix "voice." because '_' is a LIKE
	// wildcard unless escaped.
	addClaim(t, st, scopeID, "voice_x", "Underscore sibling, not in the family.")
	addClaim(t, st, scopeID, "voiceless", "No separator at all.")
	addClaim(t, st, scopeID, "hotline.channel", "Unrelated family.")
	return st, scopeID
}

func listedClaimKeys(t *testing.T, st *Store, p ListParams) []string {
	t.Helper()
	memories, err := st.List(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(memories))
	for _, mem := range memories {
		keys = append(keys, mem.ClaimKey)
	}
	return keys
}

func TestEscapeLikePrefixEscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"voice.":  "voice.",
		"voice_":  `voice\_`,
		"voice%":  `voice\%`,
		`voice\x`: `voice\\x`,
	}
	for in, want := range cases {
		if got := EscapeLikePrefix(in); got != want {
			t.Fatalf("EscapeLikePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListClaimKeyPrefixDoesNotTreatUnderscoreAsWildcard(t *testing.T) {
	st, scopeID := claimPrefixFixture(t)

	keys := listedClaimKeys(t, st, ListParams{
		ScopeKind:      "project",
		ScopeID:        scopeID,
		ClaimKeyPrefix: "voice.",
		Limit:          50,
	})
	got := map[string]bool{}
	for _, key := range keys {
		got[key] = true
	}
	if !got["voice.pillars"] || !got["voice.bubbles"] {
		t.Fatalf("expected the voice. family, got %v", keys)
	}
	if got["voice_x"] {
		t.Fatalf("prefix voice. must not match voice_x (unescaped LIKE wildcard), got %v", keys)
	}
	if got["voiceless"] || got["hotline.channel"] {
		t.Fatalf("prefix voice. matched unrelated keys: %v", keys)
	}
}

func TestListClaimKeyPrefixMatchesLiteralUnderscore(t *testing.T) {
	st, scopeID := claimPrefixFixture(t)

	keys := listedClaimKeys(t, st, ListParams{
		ScopeKind:      "project",
		ScopeID:        scopeID,
		ClaimKeyPrefix: "voice_",
		Limit:          50,
	})
	if len(keys) != 1 || keys[0] != "voice_x" {
		t.Fatalf("expected only voice_x for prefix voice_, got %v", keys)
	}
}

func TestListClaimKeyPrefixPercentIsLiteral(t *testing.T) {
	st, scopeID := claimPrefixFixture(t)

	keys := listedClaimKeys(t, st, ListParams{
		ScopeKind:      "project",
		ScopeID:        scopeID,
		ClaimKeyPrefix: "%",
		Limit:          50,
	})
	if len(keys) != 0 {
		t.Fatalf("prefix %% must be literal, not match-everything; got %v", keys)
	}
}

func TestClaimSummariesFilterByPrefixAndKey(t *testing.T) {
	st, scopeID := claimPrefixFixture(t)
	ctx := context.Background()

	summaries, err := st.ClaimSummaries(ctx, ClaimSummaryParams{
		ScopeKind:      "project",
		ScopeID:        scopeID,
		ClaimKeyPrefix: "voice.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 families under voice., got %d: %+v", len(summaries), summaries)
	}
	// ClaimSummaries orders by claim_key, which the prefix check relies on.
	if summaries[0].ClaimKey != "voice.bubbles" || summaries[1].ClaimKey != "voice.pillars" {
		t.Fatalf("expected claim_key ASC order, got %+v", summaries)
	}

	exact, err := st.ClaimSummaries(ctx, ClaimSummaryParams{
		ScopeKind: "project",
		ScopeID:   scopeID,
		ClaimKey:  "voice.pillars",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0].ClaimKey != "voice.pillars" {
		t.Fatalf("expected exactly voice.pillars, got %+v", exact)
	}
}

func TestSearchHonoursClaimKeyPrefix(t *testing.T) {
	st, scopeID := claimPrefixFixture(t)

	results, err := st.Search(context.Background(), SearchParams{
		Query:          "family sibling separator bubble plainly",
		ScopeKind:      "project",
		ScopeID:        scopeID,
		ClaimKeyPrefix: "voice.",
		Limit:          20,
		Lifecycle:      LifecycleAny,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mem := range results {
		if mem.ClaimKey != "voice.pillars" && mem.ClaimKey != "voice.bubbles" {
			t.Fatalf("search leaked a key outside the voice. family: %q", mem.ClaimKey)
		}
	}
}
