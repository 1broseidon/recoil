package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

// claimPrefixCmdFixture builds a temp project + temp DB holding a voice.* family
// plus decoys that a naive LIKE prefix would wrongly match.
func claimPrefixCmdFixture(t *testing.T) scope.Scope {
	t.Helper()
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	oldOpts := opts
	opts = globalOptions{dbPath: filepath.Join(t.TempDir(), "recoil.db"), json: true}
	t.Cleanup(func() { opts = oldOpts })

	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ key, content string }{
		{"voice.pillars", "Short words. One thought per bubble."},
		{"voice.bans", "No stock phrases like happy to help."},
		{"voice_x", "Underscore decoy that must stay out of the voice. family."},
		{"hotline.channel", "App is primary, telegram is backup."},
	} {
		if _, _, err := st.AddMemory(t.Context(), store.AddMemoryParams{
			Role:      "decision",
			Content:   row.content,
			ScopeKind: sc.Kind,
			ScopeID:   sc.ID,
			ClaimKey:  row.key,
			Validity:  "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return sc
}

func TestListClaimKeyPrefixFlagFiltersFamily(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newListCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key-prefix", "voice."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var memories []store.Memory
	if err := json.Unmarshal(data, &memories); err != nil {
		t.Fatal(err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 voice. memories, got %d", len(memories))
	}
	for _, mem := range memories {
		if !strings.HasPrefix(mem.ClaimKey, "voice.") {
			t.Fatalf("unexpected claim key %q", mem.ClaimKey)
		}
	}
}

func TestListRejectsClaimKeyAndPrefixTogether(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newListCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key", "voice.pillars", "--claim-key-prefix", "voice."})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected an error when both --claim-key and --claim-key-prefix are set")
	}
	code, exitCode := classifyError(err)
	if code != "VALIDATION" || exitCode != exitValidation {
		t.Fatalf("expected VALIDATION/%d, got %s/%d for %v", exitValidation, code, exitCode, err)
	}
}

func TestClaimsClaimKeyPrefixFlagFiltersFamilyIndex(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newClaimsCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key-prefix", "voice."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result claimsResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.ClaimKeyPrefix != "voice." {
		t.Fatalf("expected the prefix echoed back, got %q", result.ClaimKeyPrefix)
	}
	if len(result.Claims) != 2 {
		t.Fatalf("expected 2 families, got %+v", result.Claims)
	}
	if result.Claims[0].ClaimKey != "voice.bans" || result.Claims[1].ClaimKey != "voice.pillars" {
		t.Fatalf("expected claim_key ASC order, got %+v", result.Claims)
	}
}

func TestClaimsRejectsClaimKeyAndPrefixTogether(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newClaimsCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key", "voice.bans", "--claim-key-prefix", "voice."})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected an error when both --claim-key and --claim-key-prefix are set")
	}
	if code, _ := classifyError(err); code != "VALIDATION" {
		t.Fatalf("expected VALIDATION, got %s for %v", code, err)
	}
}

func TestCheckClaimKeyPrefixAuditsEveryFamily(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key-prefix", "voice."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Kind != "check_prefix_result" {
		t.Fatalf("expected check_prefix_result envelope, got %q", env.Kind)
	}
	data, _ := json.Marshal(env.Data)
	var result checkPrefixResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.FamilyCount != 2 || len(result.Families) != 2 {
		t.Fatalf("expected 2 audited families, got %+v", result)
	}
	if result.Families[0].ClaimKey != "voice.bans" || result.Families[1].ClaimKey != "voice.pillars" {
		t.Fatalf("expected claim_key ASC family order, got %+v", result.Families)
	}
	for _, family := range result.Families {
		if family.Verdict != "use" {
			t.Fatalf("expected a per-family verdict of use, got %+v", family)
		}
	}
	if result.Verdict != "use" || result.Recommendation != "use_current_decision" {
		t.Fatalf("expected aggregate use/use_current_decision, got %+v", result)
	}
}

func TestCheckClaimKeyPrefixAggregatesWorstVerdict(t *testing.T) {
	sc := claimPrefixCmdFixture(t)

	// Two current decisions in one family is the review case; the aggregate must
	// surface it even though the sibling family is clean.
	st, err := store.Open(opts.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddMemory(t.Context(), store.AddMemoryParams{
		Role:      "decision",
		Content:   "Second live decision in the same family.",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ClaimKey:  "voice.bans",
		Validity:  "active",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key-prefix", "voice."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result checkPrefixResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "review" {
		t.Fatalf("expected the worst family verdict to win, got %+v", result)
	}
	if result.DecidingClaimKey != "voice.bans" {
		t.Fatalf("expected voice.bans to be named as the deciding family, got %q", result.DecidingClaimKey)
	}
}

func TestCheckClaimKeyPrefixEmptyIsNotAnError(t *testing.T) {
	claimPrefixCmdFixture(t)

	c := newCheckCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--claim-key-prefix", "nothing."})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var result checkPrefixResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.FamilyCount != 0 || result.Verdict != "no_decision" || result.Reason != "no_claim_key_match" {
		t.Fatalf("expected an empty no_decision result, got %+v", result)
	}
}

func TestCheckRejectsPrefixWithQueryOrClaimKey(t *testing.T) {
	claimPrefixCmdFixture(t)

	for _, args := range [][]string{
		{"--claim-key", "voice.bans", "--claim-key-prefix", "voice."},
		{"--claim-key-prefix", "voice.", "should we ban stock phrases"},
	} {
		c := newCheckCommand()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		err := c.Execute()
		if err == nil {
			t.Fatalf("expected an error for args %v", args)
		}
		if code, _ := classifyError(err); code != "VALIDATION" {
			t.Fatalf("expected VALIDATION for args %v, got %s (%v)", args, code, err)
		}
	}
}

func TestMCPExposesDeterministicReadTools(t *testing.T) {
	session, cleanup := connectMCPTestServer(t, mcpOptions{})
	defer cleanup()

	result, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := mcpToolNames(result.Tools)
	for _, want := range []string{"recoil_search", "recoil_wake", "recoil_check", "recoil_list", "recoil_claims"} {
		if !names[want] {
			t.Fatalf("expected read-only tool %q to be registered, got %v", want, names)
		}
	}
	for _, unwanted := range []string{"recoil_add", "recoil_remember", "recoil_handoff"} {
		if names[unwanted] {
			t.Fatalf("write tool %q must stay behind --allow-write, got %v", unwanted, names)
		}
	}
}

func TestMCPListAndClaimsHonourPrefix(t *testing.T) {
	claimPrefixCmdFixture(t)
	ctx := t.Context()

	memories, _, err := mcpList(ctx, mcpListInput{ClaimKeyPrefix: "voice."})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories from recoil_list, got %d", len(memories))
	}

	claims, _, err := mcpClaims(ctx, mcpClaimsInput{ClaimKeyPrefix: "voice."})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Claims) != 2 {
		t.Fatalf("expected 2 families from recoil_claims, got %+v", claims.Claims)
	}

	if _, _, err := mcpList(ctx, mcpListInput{ClaimKey: "voice.bans", ClaimKeyPrefix: "voice."}); err == nil {
		t.Fatal("expected recoil_list to reject claim_key + claim_key_prefix")
	}
	if _, _, err := mcpClaims(ctx, mcpClaimsInput{ClaimKey: "voice.bans", ClaimKeyPrefix: "voice."}); err == nil {
		t.Fatal("expected recoil_claims to reject claim_key + claim_key_prefix")
	}

	result, kind, _, err := mcpCheck(ctx, mcpCheckInput{ClaimKeyPrefix: "voice."})
	if err != nil {
		t.Fatal(err)
	}
	if kind != "check_prefix_result" {
		t.Fatalf("expected check_prefix_result kind, got %q", kind)
	}
	prefixResult, ok := result.(checkPrefixResult)
	if !ok {
		t.Fatalf("expected checkPrefixResult, got %T", result)
	}
	if prefixResult.FamilyCount != 2 {
		t.Fatalf("expected 2 families, got %+v", prefixResult)
	}
}
