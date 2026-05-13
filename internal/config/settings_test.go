package config

import "testing"

func TestEffectiveValueReturnsDefaultsAndProjectOverrides(t *testing.T) {
	settings := Settings{Values: map[string]string{
		"mine.follow_repo_symlinks": "false",
	}}

	value, source, ok := EffectiveValue(settings, "mine.include_hidden_operational")
	if !ok || value != "true" || source != "default" {
		t.Fatalf("expected default hidden operational value, got value=%q source=%q ok=%t", value, source, ok)
	}

	value, source, ok = EffectiveValue(settings, "mine.follow_repo_symlinks")
	if !ok || value != "false" || source != "project" {
		t.Fatalf("expected project symlink override, got value=%q source=%q ok=%t", value, source, ok)
	}
}

func TestValidateAcceptsDynamicRankAndClassifyKeys(t *testing.T) {
	if err := Validate("classify.override.docs/runbooks/**", "operational"); err != nil {
		t.Fatal(err)
	}
	if err := Validate("rank.search.boost.operational", "1.25"); err != nil {
		t.Fatal(err)
	}
	if err := Validate("rank.search.boost.operational", "loud"); err == nil {
		t.Fatal("expected invalid float to fail")
	}
	if err := Validate("classify.override.docs/runbooks/**", "mystery"); err == nil {
		t.Fatal("expected unknown doc_class to fail")
	}
}

func TestPathListSplitsCommaSeparatedValues(t *testing.T) {
	settings := Settings{Values: map[string]string{
		"mine.include_paths": "docs/runbooks/**, ./notes/OPERATIONS",
	}}
	got := settings.PathList("mine.include_paths")
	if len(got) != 2 || got[0] != "docs/runbooks/**" || got[1] != "notes/OPERATIONS" {
		t.Fatalf("unexpected path list: %+v", got)
	}
}
