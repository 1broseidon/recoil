package sourcequality

import "testing"

func TestClassifyOperationalDocs(t *testing.T) {
	tests := []struct {
		path  string
		class string
	}{
		{"AGENTS.md", ClassAgentInstructions},
		{"CONTRIBUTING.md", ClassContributing},
		{"DEVELOPERS.md", ClassDevelopers},
		{".github/SECURITY.md", ClassSecurity},
		{"apps/docs/public/.well-known/security.txt", ClassSecurity},
		{"README.md", ClassRootReadme},
		{"examples/todo/README.md", ClassExample},
		{"CHANGELOG/CHANGELOG-1.5.md", ClassChangelog},
	}
	for _, tc := range tests {
		if got := Classify(tc.path).DocClass; got != tc.class {
			t.Fatalf("Classify(%q) = %q, want %q", tc.path, got, tc.class)
		}
	}
}

func TestScorePriorBoostsIntentSpecificOperationalDocs(t *testing.T) {
	security := ScorePrior("security policy vulnerability disclosure", ".github/SECURITY.md", "")
	example := ScorePrior("security policy vulnerability disclosure", "examples/auth/README.md", "")
	if security <= example {
		t.Fatalf("expected security doc boost above example, security=%f example=%f", security, example)
	}

	agents := ScorePrior("what should agents know before editing this repo", "AGENTS.md", "")
	fixture := ScorePrior("what should agents know before editing this repo", "evals/README.md", "")
	if agents <= fixture {
		t.Fatalf("expected agent instructions above fixture, agents=%f fixture=%f", agents, fixture)
	}
}

func TestClassifyWithOptionsAppliesPathOverrides(t *testing.T) {
	info := ClassifyWithOptions("docs/runbooks/deploy.yaml", Options{
		ClassOverrides: []ClassOverride{{Pattern: "docs/runbooks/**", DocClass: ClassOperational}},
	})
	if info.DocClass != ClassOperational || !info.IsOperationalDoc {
		t.Fatalf("expected operational override, got %+v", info)
	}
}

func TestScorePriorWithOptionsAppliesModeSpecificAdjustments(t *testing.T) {
	opts := Options{
		SearchBoosts:  map[string]float64{ClassProductDocs: 4},
		WakePenalties: map[string]float64{ClassProductDocs: 2},
	}
	search := ScorePriorWithOptions("setup docs", "docs/product/intro.md", "", ModeSearch, opts)
	wake := ScorePriorWithOptions("setup docs", "docs/product/intro.md", "", ModeWake, opts)
	if search <= wake {
		t.Fatalf("expected search boost and wake penalty to diverge, search=%f wake=%f", search, wake)
	}
}
