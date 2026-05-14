package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type locomoGraded struct {
	locomoHypothesis
	GraderModel string  `json:"grader_model"`
	GraderRaw   string  `json:"grader_raw"`
	GraderCost  float64 `json:"grader_cost_usd"`
	Correct     bool    `json:"correct"`
}

func runLoCoMoGrade(args []string) error {
	var hypPath, graderModel, resultsPath string
	var concurrency int
	var verbose bool
	fs, err := parseFlags("locomo-grade", args, func(fs *flag.FlagSet) {
		fs.StringVar(&hypPath, "hyp", "", "hypothesis JSONL produced by locomo-qa")
		fs.StringVar(&graderModel, "grader", "openai/gpt-4o-mini-2024-07-18", "grader model (OpenRouter slug)")
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

	hyps, err := loadLoCoMoHyps(hypPath)
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
		h   locomoHypothesis
	}
	jobs := make(chan job, concurrency*2)
	graded := make([]locomoGraded, len(hyps))
	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			prompt := locomoCheckPrompt(j.h)
			cctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := client.CompleteWithOptions(cctx, graderModel, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, 128, CompleteOptions{ReasoningEffort: "minimal"})
			cancel()
			g := locomoGraded{locomoHypothesis: j.h, GraderModel: graderModel}
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
				fmt.Fprintf(os.Stderr, "  %s/cat%d: correct=%v\n", j.h.SampleID, j.h.Category, g.Correct)
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

	// Aggregate.
	overall := &locomoGradeBucket{}
	cats := map[string]*locomoGradeBucket{}
	for _, g := range graded {
		// Adversarial: a "correct" abstention is a hit; otherwise apples-to-apples.
		overall.count++
		if g.Correct {
			overall.correct++
		}
		overall.cost += g.GraderCost
		cat := g.CategoryName
		c, ok := cats[cat]
		if !ok {
			c = &locomoGradeBucket{}
			cats[cat] = c
		}
		c.count++
		if g.Correct {
			c.correct++
		}
	}

	summary := map[string]any{
		"hyp":            hypPath,
		"grader":         graderModel,
		"total":          overall.count,
		"correct":        overall.correct,
		"accuracy":       safeDiv(overall.correct, overall.count),
		"grader_cost":    overall.cost,
		"by_category":    map[string]any{},
		"out":            resultsPath,
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
	mdPath := strings.TrimSuffix(resultsPath, ".jsonl") + ".md"
	if err := writeLoCoMoQAMarkdown(mdPath, hypPath, graderModel, overall.count, overall.correct, cats); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "---")
	fmt.Fprintf(os.Stdout, "hyp:        %s\n", hypPath)
	fmt.Fprintf(os.Stdout, "grader:     %s\n", graderModel)
	fmt.Fprintf(os.Stdout, "total:      %d\n", overall.count)
	fmt.Fprintf(os.Stdout, "correct:    %d\n", overall.correct)
	fmt.Fprintf(os.Stdout, "accuracy:   %.4f\n", safeDiv(overall.correct, overall.count))
	fmt.Fprintf(os.Stdout, "grader $:   %.4f\n", overall.cost)
	fmt.Fprintln(os.Stdout, "---")
	for _, k := range keys {
		c := cats[k]
		fmt.Fprintf(os.Stdout, "%-26s n=%4d correct=%4d acc=%.4f\n", k, c.count, c.correct, safeDiv(c.correct, c.count))
	}
	fmt.Fprintf(os.Stderr, "\ngraded:   %s\nsummary:  %s\nmarkdown: %s\n", resultsPath, summaryPath, mdPath)
	_ = math.NaN
	return nil
}

func loadLoCoMoHyps(path string) ([]locomoHypothesis, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []locomoHypothesis{}
	dec := json.NewDecoder(f)
	for {
		var h locomoHypothesis
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

// locomoCheckPrompt builds an LLM-as-judge prompt tuned to LoCoMo's question
// categories. Mirrors the LongMemEval grader shape so the two harnesses are
// directly comparable.
func locomoCheckPrompt(h locomoHypothesis) string {
	q := h.Question
	gold := h.GoldAnswer
	resp := h.Hypothesis
	if h.Adversarial {
		return fmt.Sprintf(
			"I will give you a question that cannot be answered from the available chat history, "+
				"an explanation of why it is unanswerable, and a response from a model. "+
				"Please answer yes if the model correctly identifies the question as unanswerable or "+
				"declines to answer due to insufficient information. Otherwise answer no.\n\n"+
				"Question: %s\n\nExplanation: %s\n\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	}
	switch h.CategoryName {
	case "temporal-reasoning":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer, otherwise no. "+
				"Do not penalize off-by-one errors when the question asks for a count of days/weeks/months.\n\n"+
				"Question: %s\nCorrect Answer: %s\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	case "multi-hop", "single-hop", "open-domain":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer. If the response is "+
				"equivalent or includes all reasoning steps, answer yes. If only a subset is correct, answer no.\n\n"+
				"Question: %s\nCorrect Answer: %s\nModel Response: %s\n\nAnswer yes or no only.",
			q, gold, resp)
	}
	return fmt.Sprintf("Question: %s\nCorrect Answer: %s\nModel Response: %s\n\nIs the response correct? Answer yes or no only.",
		q, gold, resp)
}

type locomoGradeBucket struct {
	count   int
	correct int
	cost    float64
}

func writeLoCoMoQAMarkdown(path, hypPath, grader string, total, correct int, cats map[string]*locomoGradeBucket) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# LoCoMo QA run\n\nHypotheses: %s\nGrader: %s\n\n", hypPath, grader)
	fmt.Fprintf(b, "Overall accuracy: **%.4f** (%d / %d)\n\n", safeDiv(correct, total), correct, total)
	fmt.Fprintln(b, "| category | n | correct | accuracy |")
	fmt.Fprintln(b, "|---|---|---|---|")
	keys := make([]string, 0, len(cats))
	for k := range cats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := cats[k]
		fmt.Fprintf(b, "| %s | %d | %d | %.4f |\n", k, c.count, c.correct, safeDiv(c.correct, c.count))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

var _ = filepath.Join // keep filepath import (used by writers)
