package eval

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestScoreCaseFindsMissingAndForbiddenIDs(t *testing.T) {
	tc := CaseRecord{
		ID:                  "stale-case",
		Category:            "stale_history",
		Mode:                "search",
		ExpectedCurrentIDs:  []string{"mem_current", "mem_other"},
		ForbiddenCurrentIDs: []string{"mem_old"},
		ForbiddenIDs:        []string{"mem_leak"},
	}
	result := ScoreCase(tc, []string{"mem_old", "mem_other", "mem_leak"}, nil, 12*time.Millisecond)
	if result.Passed {
		t.Fatal("expected case to fail")
	}
	if !reflect.DeepEqual(result.MissingCurrentIDs, []string{"mem_current"}) {
		t.Fatalf("unexpected missing IDs: %+v", result.MissingCurrentIDs)
	}
	if !reflect.DeepEqual(result.ForbiddenCurrentIDsPresent, []string{"mem_old"}) {
		t.Fatalf("unexpected forbidden current IDs: %+v", result.ForbiddenCurrentIDsPresent)
	}
	if !reflect.DeepEqual(result.ForbiddenIDsPresent, []string{"mem_leak"}) {
		t.Fatalf("unexpected forbidden IDs: %+v", result.ForbiddenIDsPresent)
	}
	if math.Abs(result.Recall-0.5) > 0.0001 {
		t.Fatalf("unexpected recall: %.4f", result.Recall)
	}
	if math.Abs(result.ReciprocalRank-0.5) > 0.0001 {
		t.Fatalf("unexpected reciprocal rank: %.4f", result.ReciprocalRank)
	}
	if result.LatencyMS != 12 {
		t.Fatalf("unexpected latency: %d", result.LatencyMS)
	}
}

func TestScoreCaseChecksContent(t *testing.T) {
	tc := CaseRecord{
		ID:               "content-case",
		Mode:             "search",
		ExpectedContent:  []string{"current status"},
		ForbiddenContent: []string{"stale status"},
	}
	result := ScoreCase(tc, []string{"mem_a"}, []string{"The stale status is shown."}, 1*time.Millisecond)
	if result.Passed {
		t.Fatal("expected content case to fail")
	}
	if !reflect.DeepEqual(result.MissingContent, []string{"current status"}) {
		t.Fatalf("unexpected missing content: %+v", result.MissingContent)
	}
	if !reflect.DeepEqual(result.ForbiddenContentPresent, []string{"stale status"}) {
		t.Fatalf("unexpected forbidden content: %+v", result.ForbiddenContentPresent)
	}
}

func TestScoreCaseRanksContentOnlyExpectations(t *testing.T) {
	tc := CaseRecord{
		ID:              "content-rank-case",
		Mode:            "search",
		ExpectedContent: []string{"current status", "owner alice"},
	}
	result := ScoreCase(tc, []string{"mem_noise", "mem_current"}, []string{
		"current status only",
		"current status owner alice",
	}, 1*time.Millisecond)
	if !result.Passed {
		t.Fatalf("expected ranked content case to pass: %+v", result)
	}
	if math.Abs(result.ReciprocalRank-0.5) > 0.0001 {
		t.Fatalf("unexpected content reciprocal rank: %.4f", result.ReciprocalRank)
	}
	if result.Recall != 1 {
		t.Fatalf("unexpected content recall: %.4f", result.Recall)
	}
}

func TestScoreCaseFailsContentOnlyExpectationsSplitAcrossResults(t *testing.T) {
	tc := CaseRecord{
		ID:              "split-content-rank-case",
		Mode:            "search",
		ExpectedContent: []string{"current status", "owner alice"},
	}
	result := ScoreCase(tc, []string{"mem_a", "mem_b"}, []string{
		"current status",
		"owner alice",
	}, 1*time.Millisecond)
	if result.Passed {
		t.Fatal("expected split content to fail ranked content check")
	}
	if !reflect.DeepEqual(result.MissingRankedContent, []string{"current status", "owner alice"}) {
		t.Fatalf("unexpected missing ranked content: %+v", result.MissingRankedContent)
	}
	if result.ReciprocalRank != 0 || result.Recall != 0 {
		t.Fatalf("unexpected rank/recall: %.4f %.4f", result.ReciprocalRank, result.Recall)
	}
}

func TestSummarize(t *testing.T) {
	results := []CaseResult{
		{
			ID:                 "pass",
			Mode:               "search",
			Passed:             true,
			ExpectedCurrentIDs: []string{"mem_a"},
			ReciprocalRank:     1,
		},
		{
			ID:                         "stale",
			Category:                   "stale_history",
			Mode:                       "search",
			ExpectedCurrentIDs:         []string{"mem_b"},
			MissingCurrentIDs:          []string{"mem_b"},
			ForbiddenCurrentIDsPresent: []string{"mem_old"},
		},
		{
			ID:                  "scope",
			Category:            "scope_isolation",
			Mode:                "search",
			ExpectedCurrentIDs:  []string{"mem_c"},
			ForbiddenIDsPresent: []string{"mem_other_project"},
			ReciprocalRank:      0.5,
		},
		{
			ID:            "empty",
			Mode:          "search",
			Passed:        true,
			ExpectedEmpty: true,
			EmptyOK:       true,
		},
		{
			ID:              "content-ranked",
			Mode:            "search",
			Passed:          true,
			ExpectedContent: []string{"answer"},
			ReciprocalRank:  0.25,
		},
	}
	summary := Summarize(results)
	if summary.TotalCases != 5 || summary.PassedCases != 3 || summary.FailedCases != 2 {
		t.Fatalf("unexpected pass counts: %+v", summary)
	}
	if summary.CurrentHitCount != 2 || summary.ExpectedCurrentCount != 3 {
		t.Fatalf("unexpected hit counts: %+v", summary)
	}
	if math.Abs(summary.RecallAtK-(2.0/3.0)) > 0.0001 {
		t.Fatalf("unexpected recall: %.4f", summary.RecallAtK)
	}
	if math.Abs(summary.MRR-0.4375) > 0.0001 {
		t.Fatalf("unexpected MRR: %.4f", summary.MRR)
	}
	if summary.ExpectedContentCount != 1 || summary.ContentHitCount != 1 {
		t.Fatalf("unexpected content counts: %+v", summary)
	}
	if summary.StaleDemotionFailures != 1 || summary.ScopeIsolationFailures != 1 {
		t.Fatalf("unexpected failure counts: %+v", summary)
	}
	if summary.EmptyResultAccuracy != 1 {
		t.Fatalf("unexpected empty accuracy: %.4f", summary.EmptyResultAccuracy)
	}
}
