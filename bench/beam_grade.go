package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type beamGraded struct {
	beamHypothesis
	GraderModel string  `json:"grader_model"`
	GraderRaw   string  `json:"grader_raw"`
	GraderCost  float64 `json:"grader_cost_usd"`
	Correct     bool    `json:"correct"`
}

func runBEAMGrade(args []string) error {
	var hypPath, graderModel, resultsPath string
	var concurrency int
	var verbose bool
	fs, err := parseFlags("beam-grade", args, func(fs *flag.FlagSet) {
		fs.StringVar(&hypPath, "hyp", "", "hypothesis JSONL produced by beam-qa")
		fs.StringVar(&graderModel, "grader", "openai/gpt-4o-mini-2024-07-18", "grader model")
		fs.IntVar(&concurrency, "concurrency", 8, "parallel grader calls")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write graded JSONL")
	})
	if err != nil {
		return err
	}
	_ = fs
	if hypPath == "" {
		return fmt.Errorf("--hyp is required")
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	hyps, err := loadBEAMHyps(hypPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "loaded %d hypotheses from %s\n", len(hyps), hypPath)

	if resultsPath == "" {
		resultsPath = hypPath + ".graded.jsonl"
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()

	type job struct {
		idx int
		h   beamHypothesis
	}
	jobs := make(chan job, concurrency*2)
	graded := make([]beamGraded, len(hyps))
	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			prompt := beamCheckPrompt(j.h)
			cctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := client.CompleteWithOptions(cctx, graderModel, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, 128, CompleteOptions{ReasoningEffort: "minimal"})
			cancel()
			g := beamGraded{beamHypothesis: j.h, GraderModel: graderModel}
			if err != nil {
				g.GraderRaw = "ERROR: " + err.Error()
			} else {
				g.GraderRaw = strings.TrimSpace(res.Content)
				g.GraderCost = res.CostUSD
				g.Correct = strings.Contains(strings.ToLower(g.GraderRaw), "yes")
				costMu.Lock()
				totalCost += res.CostUSD
				costMu.Unlock()
			}
			graded[j.idx] = g
			n := atomic.AddInt32(&doneCount, 1)
			if int(n)%50 == 0 || int(n) == len(hyps) {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] grader cost=$%.4f\n", n, len(hyps), totalCost)
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s/%s: correct=%v\n", j.h.Category, j.h.ConversationID, g.Correct)
			}
		}
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker()
	}
	for i, h := range hyps {
		jobs <- job{idx: i, h: h}
	}
	close(jobs)
	wg.Wait()

	enc := json.NewEncoder(resultsFile)
	for _, g := range graded {
		if err := enc.Encode(g); err != nil {
			return err
		}
	}

	type bucket struct {
		count, correct int
	}
	overall := &bucket{}
	cats := map[string]*bucket{}
	for _, g := range graded {
		overall.count++
		if g.Correct {
			overall.correct++
		}
		c, ok := cats[g.Category]
		if !ok {
			c = &bucket{}
			cats[g.Category] = c
		}
		c.count++
		if g.Correct {
			c.correct++
		}
	}
	summary := map[string]any{
		"hyp":         hypPath,
		"grader":      graderModel,
		"total":       overall.count,
		"correct":     overall.correct,
		"accuracy":    safeDiv(overall.correct, overall.count),
		"grader_cost": totalCost,
		"out":         resultsPath,
	}
	keys := make([]string, 0, len(cats))
	for k := range cats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	per := map[string]any{}
	for _, k := range keys {
		c := cats[k]
		per[k] = map[string]any{
			"count":    c.count,
			"correct":  c.correct,
			"accuracy": safeDiv(c.correct, c.count),
		}
	}
	summary["by_category"] = per
	summaryPath := strings.TrimSuffix(resultsPath, ".jsonl") + "_summary.json"
	if err := writeJSONFile(summaryPath, summary); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "---")
	fmt.Fprintf(os.Stdout, "hyp:        %s\n", hypPath)
	fmt.Fprintf(os.Stdout, "grader:     %s\n", graderModel)
	fmt.Fprintf(os.Stdout, "total:      %d\n", overall.count)
	fmt.Fprintf(os.Stdout, "correct:    %d\n", overall.correct)
	fmt.Fprintf(os.Stdout, "accuracy:   %.4f\n", safeDiv(overall.correct, overall.count))
	fmt.Fprintf(os.Stdout, "grader $:   %.4f\n", totalCost)
	fmt.Fprintln(os.Stdout, "---")
	for _, k := range keys {
		c := cats[k]
		fmt.Fprintf(os.Stdout, "%-26s n=%4d correct=%4d acc=%.4f\n", k, c.count, c.correct, safeDiv(c.correct, c.count))
	}
	fmt.Fprintf(os.Stderr, "\ngraded:  %s\nsummary: %s\n", resultsPath, summaryPath)
	return nil
}

func loadBEAMHyps(path string) ([]beamHypothesis, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []beamHypothesis{}
	dec := json.NewDecoder(f)
	for {
		var h beamHypothesis
		if err := dec.Decode(&h); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// beamCheckPrompt mirrors LongMemEval grader shape, tuned per BEAM category.
func beamCheckPrompt(h beamHypothesis) string {
	q := h.Question
	resp := h.Hypothesis
	gold := h.IdealAnswer
	if gold == "" {
		gold = h.IdealResponse
	}
	switch h.Category {
	case "abstention":
		return fmt.Sprintf(
			"I will give you a question that cannot be answered from the available chat history, "+
				"an ideal abstaining response, and a response from a model. "+
				"Please answer yes if the model correctly identifies the question as unanswerable, declines to "+
				"answer, or states that the information is unavailable. Otherwise answer no.\n\n"+
				"Question: %s\n\nIdeal Response: %s\n\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	case "contradiction_resolution":
		return fmt.Sprintf(
			"I will give you a question, an ideal response that flags a contradiction and asks for clarification, "+
				"and a response from a model. Please answer yes if the model detects a contradiction or asks the user "+
				"to clarify. Otherwise answer no.\n\n"+
				"Question: %s\n\nIdeal Response: %s\n\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	case "temporal_reasoning", "event_ordering":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer. "+
				"Do not penalize off-by-one errors when the question asks for a count of days/weeks/months or for ordering.\n\n"+
				"Question: %s\nCorrect Answer: %s\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	}
	return fmt.Sprintf(
		"I will give you a question, a correct answer, and a response from a model. "+
			"Please answer yes if the response contains the correct answer. If the response is equivalent "+
			"or covers the required reasoning, answer yes. If only a subset is correct, answer no.\n\n"+
			"Question: %s\nCorrect Answer: %s\nModel Response: %s\n\nAnswer yes or no only.",
		q, gold, resp)
}
