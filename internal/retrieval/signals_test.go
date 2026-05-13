package retrieval

import (
	"strings"
	"testing"
	"time"
)

func TestProfileTextEmitsInterestTrace(t *testing.T) {
	text := ProfileText("I am working in deep learning for medical image analysis and I enjoy recent healthcare AI papers.")
	for _, want := range []string{"user_interest", "publications", "conferences", "deep", "medical", "image", "analysi"} {
		if !strings.Contains(text, want) {
			t.Fatalf("profile text missing %q in %q", want, text)
		}
	}
}

func TestProfileTextStaysQuietForGenericPrompt(t *testing.T) {
	if got := ProfileText("Can you explain how merge sort works?"); got != "" {
		t.Fatalf("expected no profile text, got %q", got)
	}
}

func TestProfileTextAddsDomainTags(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"I am looking to organize my new portable power bank and wireless charging pad.", "battery"},
		{"I still remember high school debate team and advanced placement courses.", "reunion"},
		{"Can you suggest recipes with fresh basil and mint?", "homegrown"},
	}
	for _, tc := range cases {
		if got := ProfileText(tc.text); !strings.Contains(got, tc.want) {
			t.Fatalf("ProfileText(%q) = %q, want tag %q", tc.text, got, tc.want)
		}
	}
}

func TestUpdateTextEmitsCurrentStateTrace(t *testing.T) {
	text := UpdateText("We changed the SQLite driver to mattn/go-sqlite3 and no longer use modernc.")
	for _, want := range []string{"current_state", "latest_update", "no_longer", "sqlite", "mattn", "modernc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("update text missing %q in %q", want, text)
		}
	}
}

func TestUpdateTextStaysQuietForGenericPrompt(t *testing.T) {
	if got := UpdateText("Can you explain how SQLite works?"); got != "" {
		t.Fatalf("expected no update text, got %q", got)
	}
}

func TestExpandedQueryTextAddsEntityAliases(t *testing.T) {
	text := ExpandedQueryText("How many different doctors did I visit?")
	for _, want := range []string{"physician", "dermatologist", "primary", "provider"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expanded query missing %q in %q", want, text)
		}
	}

	if got := ExpandedQueryText("auth keyring"); got != "auth keyring" {
		t.Fatalf("unexpected expansion for ordinary query: %q", got)
	}
}

func TestExpandedQueryTextAddsCountingAliases(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"What is the total number of siblings I have?", "brother"},
		{"What kitchen appliance did I buy 10 days ago?", "smoker"},
		{"How many times did I bake something?", "cookies"},
		{"What is the total distance of my hikes?", "miles"},
		{"What is the order of the three sports events?", "tournament"},
		{"How many years older am I than when I graduated from college?", "bachelor"},
		{"What is the current session evidence min chars default?", "minimum"},
		{"How old am I compared to graduation from college?", "currently"},
	}
	for _, tc := range cases {
		if got := ExpandedQueryText(tc.query); !strings.Contains(got, tc.want) {
			t.Fatalf("ExpandedQueryText(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestQueryVariantsIncludesOriginalFragmentsAndExpansion(t *testing.T) {
	variants := QueryVariants("Which doctor discussed biopsy and which doctor handled the UTI?")
	joined := strings.Join(variants, "\n")
	for _, want := range []string{
		"Which doctor discussed biopsy and which doctor handled the UTI?",
		"dermatologist",
		"Which doctor discussed biopsy",
		"which doctor handled the UTI",
		"doctor discuss biopsy handl uti",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("variants missing %q in:\n%s", want, joined)
		}
	}
}

func TestQueryVariantsSplitsComparedQuestions(t *testing.T) {
	variants := QueryVariants("how old am I compared to graduation from college")
	joined := strings.Join(variants, "\n")
	for _, want := range []string{"how old am I", "graduation from college", "currently"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("variants missing %q in:\n%s", want, joined)
		}
	}
}

func TestDoctorNameTermsExtractsExplicitDrNames(t *testing.T) {
	got := strings.Join(DoctorNameTerms("Dr. Alvarez cardiologist appointment and Dr Smith"), " ")
	for _, want := range []string{"alvarez", "smith"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor terms missing %q in %q", want, got)
		}
	}
}

func TestTemporalScoreRelativeTarget(t *testing.T) {
	queryDate := time.Date(2023, 3, 28, 12, 0, 0, 0, time.UTC)
	target := time.Date(2023, 3, 1, 12, 0, 0, 0, time.UTC)
	far := time.Date(2023, 2, 1, 12, 0, 0, 0, time.UTC)
	if TemporalScore(queryDate, target, "what business milestone did I mention four weeks ago", "", "temporal-reasoning", 0) <= TemporalScore(queryDate, far, "what business milestone did I mention four weeks ago", "", "temporal-reasoning", 0) {
		t.Fatal("relative target date should score above unrelated date")
	}
}

func TestTemporalScoreNumericDaysAgo(t *testing.T) {
	queryDate := time.Date(2023, 3, 25, 12, 0, 0, 0, time.UTC)
	target := time.Date(2023, 3, 15, 12, 0, 0, 0, time.UTC)
	far := time.Date(2023, 3, 20, 12, 0, 0, 0, time.UTC)
	if TemporalScore(queryDate, target, "what kitchen appliance did I buy 10 days ago", "I got a smoker today", "temporal-reasoning", 0) <= TemporalScore(queryDate, far, "what kitchen appliance did I buy 10 days ago", "I got a smoker today", "temporal-reasoning", 0) {
		t.Fatal("numeric days-ago target should score above unrelated date")
	}
}

func TestTemporalScoreGatesNonTemporalDateBoostByLexicalEvidence(t *testing.T) {
	queryDate := time.Date(2023, 5, 30, 12, 0, 0, 0, time.UTC)
	sourceDate := time.Date(2023, 5, 22, 12, 0, 0, 0, time.UTC)
	noEvidence := TemporalScore(queryDate, sourceDate, "what game did I finally beat last weekend", "", "single-session-user", 0)
	withEvidence := TemporalScore(queryDate, sourceDate, "what game did I finally beat last weekend", "", "single-session-user", 0.02)
	if noEvidence != 0 {
		t.Fatalf("expected no non-temporal temporal boost without lexical evidence, got %f", noEvidence)
	}
	if withEvidence <= noEvidence {
		t.Fatalf("expected lexical evidence to allow temporal boost, got no=%f with=%f", noEvidence, withEvidence)
	}
}

func TestTemporalScoreCanUseSourceTextForWindowLanguage(t *testing.T) {
	queryDate := time.Date(2023, 5, 30, 12, 0, 0, 0, time.UTC)
	sourceDate := time.Date(2023, 5, 22, 12, 0, 0, 0, time.UTC)
	got := TemporalScore(queryDate, sourceDate, "what game did I finally beat", "last weekend I beat Celeste", "single-session-user", 0.02)
	if got <= 0 {
		t.Fatalf("expected source-side temporal language to contribute inside gated topical match, got %f", got)
	}
}
