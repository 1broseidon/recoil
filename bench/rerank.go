package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rerankCandidates asks an LLM to rank retrieved sessions by relevance to the
// question and return the top N. The reranker only sees the question text and
// the candidates — never the ground-truth answer_session_ids — so this is
// directly equivalent to what recoil would run in production.
//
// Implementation notes:
//   - The prompt presents candidates as numbered list items with their date
//     and a JSON dump of the turns. We give the model the same view recoil
//     itself stores; no summarization, no preprocessing.
//   - Response format is a comma-separated list of integers. We parse with a
//     regex and accept partial responses gracefully (e.g. if the model returns
//     "3, 7, 12" we keep those three).
//   - On any error we return (nil, 0, err) and the caller falls back to the
//     unranked candidates rather than failing the question.
func rerankCandidates(client *OpenRouterClient, rerankModel string, q lmeQuestion, candidates []contextSession, topN int) ([]int, float64, error) {
	if topN <= 0 || len(candidates) == 0 {
		return nil, 0, nil
	}
	if topN >= len(candidates) {
		out := make([]int, len(candidates))
		for i := range out {
			out[i] = i
		}
		return out, 0, nil
	}

	prompt := buildRerankPrompt(q, candidates, topN)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Reasoning models (deepseek-v4-flash, gpt-5.x) emit hidden reasoning that
	// counts against max_tokens; 256 isn't enough for the model to reason about
	// 20 candidates AND emit the index list. 1024 lets the reasoning play out
	// and still produce indices. We avoid setting reasoning_effort=minimal here
	// because some models interpret that as "skip output entirely" when the
	// reasoning fills the budget — let the provider default apply.
	res, err := client.Complete(ctx, rerankModel,
		[]ChatMessage{{Role: "user", Content: prompt}},
		0.0, 1024)
	if err != nil {
		return nil, 0, err
	}
	picked := parseRerankResponse(res.Content, len(candidates), topN)
	if len(picked) == 0 {
		return nil, res.CostUSD, fmt.Errorf("rerank returned no parsable indices: %q", strings.TrimSpace(res.Content))
	}
	return picked, res.CostUSD, nil
}

// buildRerankPrompt mirrors the answerer prompt's structure (numbered sessions
// with date headers + JSON turns) so the reranker sees what the answerer would
// see. The instruction is intentionally simple: pick the most relevant N.
func buildRerankPrompt(q lmeQuestion, candidates []contextSession, topN int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are selecting the most relevant chat sessions to help answer a question.\n\n")
	fmt.Fprintf(&b, "Question: %s\n", q.Question)
	if q.QuestionDate != "" {
		fmt.Fprintf(&b, "Question date: %s\n", q.QuestionDate)
	}
	fmt.Fprintf(&b, "\nCandidate sessions:\n")
	for i, c := range candidates {
		raw, _ := json.Marshal(c.Turns)
		fmt.Fprintf(&b, "\n[%d] Session date: %s\n%s\n", i+1, c.Date, string(raw))
	}
	fmt.Fprintf(&b, "\nReturn ONLY the numbers of the up to %d most relevant sessions, ", topN)
	fmt.Fprintf(&b, "comma-separated, in order of relevance (most relevant first). ")
	fmt.Fprintf(&b, "Example response format: 3, 7, 12\n")
	fmt.Fprintf(&b, "Do not include any other text. Sessions that are clearly irrelevant should be omitted entirely.\n")
	return b.String()
}

var rerankNumRE = regexp.MustCompile(`\d+`)

// parseRerankResponse extracts integers from the model's response, converts
// them to 0-based indices, dedupes, bounds-checks, and caps at topN.
// Defensive against verbose responses, prefix text, or trailing commentary —
// we just take whatever numbers appear, in order, that fall within range.
func parseRerankResponse(content string, candidateCount, topN int) []int {
	matches := rerankNumRE.FindAllString(content, -1)
	seen := map[int]bool{}
	picked := make([]int, 0, topN)
	for _, m := range matches {
		n, err := strconv.Atoi(m)
		if err != nil {
			continue
		}
		// 1-based in the prompt → 0-based here
		idx := n - 1
		if idx < 0 || idx >= candidateCount {
			continue
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		picked = append(picked, idx)
		if len(picked) >= topN {
			break
		}
	}
	return picked
}
