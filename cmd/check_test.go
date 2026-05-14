package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestCheckCommandReviewsRejectedDecisionWithReceipts(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Decided against Redis for cache because ops cost was not justified at current scale.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "rejected",
		ClaimKey:     "dependency.cache.redis",
		MetadataJSON: `{"predicate":{"tier":"semantic","kind":"semantic","holds_while":"ops cost remains unjustified at current scale","recheck_prompt":"Has scale or cost picture changed?"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"add Redis for this cache"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: review",
		"predicate_status: unknown",
		"recommendation: ask_operator",
		"reason: matched_historical_decision",
		"claim_key: dependency.cache.redis",
		"Has scale or cost picture changed?",
		"Decided against Redis",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestCheckCommandDoesNotReviewUnknownPredicateAlone(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Keep Recoil local-first for v0.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "architecture.local-first",
		MetadataJSON: `{"predicate":{"tier":"semantic","kind":"semantic","holds_while":"local single-user behavior remains the product target"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"local first product target"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: use",
		"predicate_status: unknown",
		"recommendation: use_current_decision",
		"claim_key: architecture.local-first",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "verdict: review") {
		t.Fatalf("did not expect review for unknown predicate alone:\n%s", got)
	}
}

func TestCheckCommandReviewsBrokenValidUntilPredicate(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Avoid pattern Y until lib Z bug B is fixed.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "dependency.lib-z-pattern",
		MetadataJSON: `{"predicate":{"tier":"deterministic","kind":"valid_until","valid_until":"2000-01-01","recheck_prompt":"Revalidate lib Z bug B."}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"pattern Y lib Z"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: review",
		"predicate_status: broken",
		"recommendation: revalidate",
		"reason: valid_until_expired",
		"Revalidate lib Z bug B.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestCheckCommandReviewsExternalPredicate(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Avoid pattern Y while lib Z is below v4.0.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "dependency.lib-z-pattern",
		MetadataJSON: `{"predicate":{"tier":"external","kind":"package_version_lt","evaluator":"package_version_lt","subject":"lib_z","operator":"<","value":"4.0","prompt":"Check current lib Z version."}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"pattern Y lib Z"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: review",
		"predicate_status: needs_review",
		"recommendation: revalidate",
		"reason: external_predicate",
		"Check current lib Z version.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestCheckCommandReviewsContradictingStancedDecision(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Reject Redis for cache because ops cost is not justified.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "dependency.cache.redis",
		MetadataJSON: `{"stance":"rejects","subject":"Redis","predicate":{"tier":"semantic","kind":"semantic","recheck_prompt":"Has scale changed?"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"use Redis for cache"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: review",
		"recommendation: ask_operator",
		"reason: request_contradicts_decision",
		"decision_stance: rejects",
		"decision_subject: Redis",
		"requested_action: adopt_subject",
		"Has scale changed?",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestCheckCommandUsesAlignedStancedDecisionWithAdvisory(t *testing.T) {
	root, dbPath := setupCheckProject(t)
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:         "decision",
		Content:      "Reject Redis while ops cost remains unjustified.",
		ScopeKind:    sc.Kind,
		ScopeID:      sc.ID,
		ProjectID:    sc.ProjectID,
		Validity:     "active",
		ClaimKey:     "dependency.cache.redis",
		MetadataJSON: `{"stance":"rejects","subject":"Redis","predicate":{"tier":"deterministic","kind":"valid_until","valid_until":"2999-01-01","holds_while":"ops cost remains unjustified","recheck_prompt":"Has cost changed?"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"avoid Redis for cache"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"verdict: use",
		"predicate_status: holds",
		"predicate_reason: valid_until_holds",
		"reason: request_consistent_with_decision",
		"requested_action: remove_subject",
		"advisory: ops cost remains unjustified",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func setupCheckProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dbPath := filepath.Join(t.TempDir(), "recoil.db")
	oldOpts := opts
	opts = globalOptions{dbPath: dbPath}
	t.Cleanup(func() { opts = oldOpts })
	return root, dbPath
}
