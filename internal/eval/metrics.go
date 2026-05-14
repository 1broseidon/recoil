package eval

import (
	"strings"
	"time"
)

type CaseResult struct {
	ID                         string   `json:"id"`
	Category                   string   `json:"category,omitempty"`
	Mode                       string   `json:"mode"`
	Passed                     bool     `json:"passed"`
	ExpectedEmpty              bool     `json:"expected_empty"`
	EmptyOK                    bool     `json:"empty_ok"`
	ResultIDs                  []string `json:"result_ids"`
	ExpectedCurrentIDs         []string `json:"expected_current_ids,omitempty"`
	ExpectedContent            []string `json:"expected_content,omitempty"`
	MissingCurrentIDs          []string `json:"missing_current_ids,omitempty"`
	ForbiddenIDsPresent        []string `json:"forbidden_ids_present,omitempty"`
	ForbiddenCurrentIDsPresent []string `json:"forbidden_current_ids_present,omitempty"`
	MissingContent             []string `json:"missing_content,omitempty"`
	MissingRankedContent       []string `json:"missing_ranked_content,omitempty"`
	ForbiddenContentPresent    []string `json:"forbidden_content_present,omitempty"`
	Recall                     float64  `json:"recall"`
	ReciprocalRank             float64  `json:"reciprocal_rank"`
	LatencyMS                  int64    `json:"latency_ms"`
}

type Summary struct {
	TotalCases             int     `json:"total_cases"`
	PassedCases            int     `json:"passed_cases"`
	FailedCases            int     `json:"failed_cases"`
	RecallAtK              float64 `json:"recall_at_k"`
	CurrentHitCount        int     `json:"current_hit_count"`
	ExpectedCurrentCount   int     `json:"expected_current_count"`
	ContentHitCount        int     `json:"content_hit_count"`
	ExpectedContentCount   int     `json:"expected_content_count"`
	MRR                    float64 `json:"mrr"`
	RankedCaseCount        int     `json:"ranked_case_count"`
	EmptyCaseCount         int     `json:"empty_case_count"`
	EmptyCorrectCount      int     `json:"empty_correct_count"`
	EmptyResultAccuracy    float64 `json:"empty_result_accuracy"`
	ScopeIsolationFailures int     `json:"scope_isolation_failures"`
	StaleDemotionFailures  int     `json:"stale_demotion_failures"`
	WakeSafetyFailures     int     `json:"wake_safety_failures"`
	WrongMemoryFailures    int     `json:"wrong_memory_failures"`
	RankedContentFailures  int     `json:"ranked_content_failures"`
	LatencyMS              int64   `json:"latency_ms"`
}

func ScoreCase(tc CaseRecord, resultIDs []string, resultTexts []string, elapsed time.Duration) CaseResult {
	resultText := strings.Join(resultTexts, "\n")
	resultSet := stringSet(resultIDs)
	missing := missingIDs(tc.ExpectedCurrentIDs, resultSet)
	forbidden := presentIDs(tc.ForbiddenIDs, resultSet)
	forbiddenCurrent := presentIDs(tc.ForbiddenCurrentIDs, resultSet)
	missingContent := missingContent(tc.ExpectedContent, resultText)
	forbiddenContent := presentContent(tc.ForbiddenContent, resultText)
	rankedContentMissing := missingRankedContent(tc.ExpectedContent, resultTexts, missingContent)

	recall := 1.0
	if len(tc.ExpectedCurrentIDs) > 0 {
		recall = float64(len(tc.ExpectedCurrentIDs)-len(missing)) / float64(len(tc.ExpectedCurrentIDs))
	} else if len(tc.ExpectedContent) > 0 {
		recall = 0
		if len(rankedContentMissing) == 0 && len(missingContent) == 0 {
			recall = 1
		}
	}
	reciprocalRank := reciprocalRank(tc.ExpectedCurrentIDs, resultIDs)
	if len(tc.ExpectedCurrentIDs) == 0 && len(tc.ExpectedContent) > 0 {
		reciprocalRank = contentReciprocalRank(tc.ExpectedContent, resultTexts)
	}
	emptyOK := true
	if tc.ExpectedEmpty {
		emptyOK = len(resultIDs) == 0
	}
	passed := len(missing) == 0 &&
		len(forbidden) == 0 &&
		len(forbiddenCurrent) == 0 &&
		len(missingContent) == 0 &&
		len(rankedContentMissing) == 0 &&
		len(forbiddenContent) == 0 &&
		emptyOK

	return CaseResult{
		ID:                         tc.ID,
		Category:                   tc.Category,
		Mode:                       tc.Mode,
		Passed:                     passed,
		ExpectedEmpty:              tc.ExpectedEmpty,
		EmptyOK:                    emptyOK,
		ResultIDs:                  append([]string(nil), resultIDs...),
		ExpectedCurrentIDs:         append([]string(nil), tc.ExpectedCurrentIDs...),
		ExpectedContent:            append([]string(nil), tc.ExpectedContent...),
		MissingCurrentIDs:          missing,
		ForbiddenIDsPresent:        forbidden,
		ForbiddenCurrentIDsPresent: forbiddenCurrent,
		MissingContent:             missingContent,
		MissingRankedContent:       rankedContentMissing,
		ForbiddenContentPresent:    forbiddenContent,
		Recall:                     recall,
		ReciprocalRank:             reciprocalRank,
		LatencyMS:                  elapsed.Milliseconds(),
	}
}

func Summarize(results []CaseResult) Summary {
	var summary Summary
	for _, result := range results {
		summary.TotalCases++
		summary.LatencyMS += result.LatencyMS
		if result.Passed {
			summary.PassedCases++
		}
		summary.ExpectedCurrentCount += len(result.ExpectedCurrentIDs)
		summary.CurrentHitCount += len(result.ExpectedCurrentIDs) - len(result.MissingCurrentIDs)
		summary.ExpectedContentCount += len(result.ExpectedContent)
		summary.ContentHitCount += len(result.ExpectedContent) - len(result.MissingContent)
		if len(result.ExpectedCurrentIDs) > 0 || len(result.ExpectedContent) > 0 {
			summary.RankedCaseCount++
			summary.MRR += result.ReciprocalRank
		}
		if result.ExpectedEmpty {
			summary.EmptyCaseCount++
			if result.EmptyOK {
				summary.EmptyCorrectCount++
			}
		}
		if result.Category == "scope_isolation" && len(result.ForbiddenIDsPresent) > 0 {
			summary.ScopeIsolationFailures++
		}
		if len(result.ForbiddenCurrentIDsPresent) > 0 {
			summary.StaleDemotionFailures++
		}
		if (result.Mode == "wake" || result.Category == "wake_safety") &&
			(len(result.ForbiddenIDsPresent) > 0 || len(result.ForbiddenCurrentIDsPresent) > 0) {
			summary.WakeSafetyFailures++
		}
		if len(result.ForbiddenContentPresent) > 0 {
			summary.WrongMemoryFailures++
		}
		if len(result.MissingRankedContent) > 0 {
			summary.RankedContentFailures++
		}
	}
	summary.FailedCases = summary.TotalCases - summary.PassedCases
	if summary.ExpectedCurrentCount > 0 {
		summary.RecallAtK = float64(summary.CurrentHitCount) / float64(summary.ExpectedCurrentCount)
	}
	if summary.RankedCaseCount > 0 {
		summary.MRR = summary.MRR / float64(summary.RankedCaseCount)
	}
	if summary.EmptyCaseCount > 0 {
		summary.EmptyResultAccuracy = float64(summary.EmptyCorrectCount) / float64(summary.EmptyCaseCount)
	}
	return summary
}

func missingContent(expected []string, resultText string) []string {
	lower := strings.ToLower(resultText)
	var missing []string
	for _, value := range expected {
		if !strings.Contains(lower, strings.ToLower(value)) {
			missing = append(missing, value)
		}
	}
	return missing
}

func presentContent(forbidden []string, resultText string) []string {
	lower := strings.ToLower(resultText)
	var present []string
	for _, value := range forbidden {
		if strings.Contains(lower, strings.ToLower(value)) {
			present = append(present, value)
		}
	}
	return present
}

func missingRankedContent(expected, resultTexts, missingAny []string) []string {
	if len(expected) == 0 || len(missingAny) > 0 {
		return nil
	}
	if contentReciprocalRank(expected, resultTexts) > 0 {
		return nil
	}
	return append([]string(nil), expected...)
}

func contentReciprocalRank(expected, resultTexts []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	for i, text := range resultTexts {
		if len(missingContent(expected, text)) == 0 {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func missingIDs(expected []string, present map[string]bool) []string {
	var missing []string
	for _, id := range expected {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func presentIDs(expected []string, present map[string]bool) []string {
	var found []string
	for _, id := range expected {
		if present[id] {
			found = append(found, id)
		}
	}
	return found
}

func reciprocalRank(expected, results []string) float64 {
	expectedSet := stringSet(expected)
	if len(expectedSet) == 0 {
		return 0
	}
	for i, id := range results {
		if expectedSet[id] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}
