package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/1broseidon/recoil/internal/scope"
	"github.com/1broseidon/recoil/internal/store"
)

func TestEvaluateSourceUnchangedPredicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.md")
	writeCmdTestFile(t, path, "original decision source\n")
	sum := sha256.Sum256([]byte("original decision source\n"))
	pred := decisionPredicate{
		Tier:       "deterministic",
		Kind:       "source_unchanged",
		SourcePath: path,
		SourceHash: hex.EncodeToString(sum[:]),
	}
	if got := evaluateSourceUnchanged(pred); got.Status != "holds" {
		t.Fatalf("expected source predicate to hold, got %+v", got)
	}
	writeCmdTestFile(t, path, "changed decision source\n")
	if got := evaluateSourceUnchanged(pred); got.Status != "broken" || got.Reason != "source_hash_changed" {
		t.Fatalf("expected changed source predicate to break, got %+v", got)
	}
}

func TestEvaluateClaimKeyStatusPredicate(t *testing.T) {
	root := t.TempDir()
	if _, err := scope.InitProject(root); err != nil {
		t.Fatal(err)
	}
	sc, err := scope.ProjectScope(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "recoil.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, _, err := st.AddMemory(context.Background(), store.AddMemoryParams{
		Role:      "decision",
		Content:   "Dependency posture remains active.",
		ScopeKind: sc.Kind,
		ScopeID:   sc.ID,
		ProjectID: sc.ProjectID,
		Validity:  "active",
		ClaimKey:  "dependency.posture",
	}); err != nil {
		t.Fatal(err)
	}
	pred := decisionPredicate{
		Tier:     "deterministic",
		Kind:     "claim_key_status",
		ClaimKey: "dependency.posture",
		Validity: "active",
	}
	if got := evaluateClaimKeyStatus(context.Background(), st, sc, pred); got.Status != "holds" {
		t.Fatalf("expected claim-key predicate to hold, got %+v", got)
	}
	pred.Validity = "rejected"
	if got := evaluateClaimKeyStatus(context.Background(), st, sc, pred); got.Status != "broken" {
		t.Fatalf("expected missing claim-key status to break, got %+v", got)
	}
}

func TestBuildDecisionPredicateParsesExternalPredicate(t *testing.T) {
	pred, ok, err := buildDecisionPredicate(predicateOptions{
		raw: "kind=external,evaluator=package_version_lt,subject=lib_z,operator=<,value=4.0,prompt=Check lib Z version",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected predicate")
	}
	if pred.Tier != "external" || pred.Evaluator != "package_version_lt" || pred.Status != "unknown" {
		t.Fatalf("unexpected predicate: %+v", pred)
	}
}

func TestBuildDecisionPredicateNormalizesDateExpiryAlias(t *testing.T) {
	pred, ok, err := buildDecisionPredicate(predicateOptions{
		raw:        "kind=date_expiry",
		validUntil: "2999-01-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || pred.Kind != "valid_until" || pred.ValidUntil != "2999-01-01" {
		t.Fatalf("expected date_expiry alias to normalize to valid_until, got %+v", pred)
	}
}

func TestBuildDecisionPredicateRejectsConflictingValidUntilKind(t *testing.T) {
	_, _, err := buildDecisionPredicate(predicateOptions{
		raw:        "kind=package_version,subject=go,operator=>=,value=1.22",
		validUntil: "2999-01-01",
	})
	if err == nil {
		t.Fatal("expected conflicting predicate kind error")
	}
}

func TestEvaluatePackageVersionPredicate(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.22\n")
	pred := decisionPredicate{
		Tier:     "deterministic",
		Kind:     "package_version",
		Subject:  "go",
		Operator: ">=",
		Value:    "1.20",
	}
	got := evaluatePackageVersion(scope.Scope{Root: root}, pred)
	if got.Status != "holds" || got.Reason != "package_version_matches" {
		t.Fatalf("expected package version to hold, got %+v", got)
	}
	pred.Value = "1.23"
	got = evaluatePackageVersion(scope.Scope{Root: root}, pred)
	if got.Status != "broken" || got.Reason != "package_version_mismatch" {
		t.Fatalf("expected package version mismatch, got %+v", got)
	}
}

func TestEvaluatePythonVersionPredicateUsesPackageVersionEvaluator(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "pyproject.toml"), `requires-python = ">=3.10"`)
	pred := normalizeDecisionPredicate(decisionPredicate{
		Tier:     "deterministic",
		Kind:     "python_version",
		Operator: ">=",
		Value:    "3.10",
	})
	if pred.Kind != "package_version" || pred.Subject != "requires-python" {
		t.Fatalf("expected python_version to normalize to package_version/requires-python, got %+v", pred)
	}
	got := evaluatePackageVersion(scope.Scope{Root: root}, pred)
	if got.Status != "holds" || got.Reason != "package_version_matches" {
		t.Fatalf("expected python version predicate to hold, got %+v", got)
	}
}

func TestEvaluateTSConfigValuePredicate(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, "base.json"), `{"compilerOptions":{"strict":true}}`)
	writeCmdTestFile(t, filepath.Join(root, "tsconfig.json"), `{"extends":"./base.json","compilerOptions":{"noImplicitAny":true}}`)
	pred := decisionPredicate{
		Tier:     "deterministic",
		Kind:     "tsconfig_value",
		Subject:  "compilerOptions.strict",
		Operator: "==",
		Value:    "true",
	}
	got := evaluateTSConfigValue(scope.Scope{Root: root}, pred)
	if got.Status != "holds" || got.Reason != "tsconfig_value_matches" {
		t.Fatalf("expected tsconfig value to hold, got %+v", got)
	}
}

func TestEvaluateLintConfigValuePredicate(t *testing.T) {
	root := t.TempDir()
	writeCmdTestFile(t, filepath.Join(root, ".golangci.yml"), "linters-settings:\n  gocyclo:\n    max-complexity: 12\n")
	pred := decisionPredicate{
		Tier:     "deterministic",
		Kind:     "lint_config_value",
		Subject:  "linters-settings.gocyclo.max-complexity",
		Operator: "<=",
		Value:    "15",
	}
	got := evaluateLintConfigValue(scope.Scope{Root: root}, pred)
	if got.Status != "holds" || got.Reason != "lint_config_value_matches" {
		t.Fatalf("expected lint config value to hold, got %+v", got)
	}
}
