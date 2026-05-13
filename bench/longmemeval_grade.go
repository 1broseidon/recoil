package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// autoEvalLabel matches the paper's evaluate_qa.py output schema so our results
// remain consumable by their tooling. The Model field records which grader
// produced the label.
type autoEvalLabel struct {
	Model string `json:"model"`
	Label bool   `json:"label"`
}

type graderRecord struct {
	answererHypothesis
	AutoEvalLabel autoEvalLabel `json:"autoeval_label"`
	GraderRaw     string        `json:"grader_raw"`
	GraderCost    float64       `json:"grader_cost"`
}

// answerCheckPrompt mirrors evaluate_qa.py:get_anscheck_prompt verbatim. The
// templates are intentionally not deduplicated — they differ in subtle ways
// (off-by-one tolerance for temporal, "previous + updated" allowance for
// knowledge-update, rubric framing for preference) and matching the paper
// exactly is the only way our QA numbers stay comparable to published ones.
func answerCheckPrompt(qtype, question, answer, response string, abstention bool) string {
	if abstention {
		return fmt.Sprintf(
			"I will give you an unanswerable question, an explanation, and a response from a model. "+
				"Please answer yes if the model correctly identifies the question as unanswerable. "+
				"The model could say that the information is incomplete, or some other information is given "+
				"but the asked information is not.\n\nQuestion: %s\n\nExplanation: %s\n\nModel Response: %s\n\n"+
				"Does the model correctly identify the question as unanswerable? Answer yes or no only.",
			question, answer, response)
	}
	switch qtype {
	case "single-session-user", "single-session-assistant", "multi-session":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer. Otherwise, answer no. "+
				"If the response is equivalent to the correct answer or contains all the intermediate steps "+
				"to get the correct answer, you should also answer yes. If the response only contains a subset "+
				"of the information required by the answer, answer no. \n\nQuestion: %s\n\nCorrect Answer: %s\n\n"+
				"Model Response: %s\n\nIs the model response correct? Answer yes or no only.",
			question, answer, response)
	case "temporal-reasoning":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer. Otherwise, answer no. "+
				"If the response is equivalent to the correct answer or contains all the intermediate steps "+
				"to get the correct answer, you should also answer yes. If the response only contains a subset "+
				"of the information required by the answer, answer no. In addition, do not penalize off-by-one "+
				"errors for the number of days. If the question asks for the number of days/weeks/months, etc., "+
				"and the model makes off-by-one errors (e.g., predicting 19 days when the answer is 18), the "+
				"model's response is still correct. \n\nQuestion: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\n"+
				"Is the model response correct? Answer yes or no only.",
			question, answer, response)
	case "knowledge-update":
		return fmt.Sprintf(
			"I will give you a question, a correct answer, and a response from a model. "+
				"Please answer yes if the response contains the correct answer. Otherwise, answer no. "+
				"If the response contains some previous information along with an updated answer, the response "+
				"should be considered as correct as long as the updated answer is the required answer.\n\n"+
				"Question: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.",
			question, answer, response)
	case "single-session-preference":
		return fmt.Sprintf(
			"I will give you a question, a rubric for desired personalized response, and a response from a model. "+
				"Please answer yes if the response satisfies the desired response. Otherwise, answer no. "+
				"The model does not need to reflect all the points in the rubric. The response is correct as long "+
				"as it recalls and utilizes the user's personal information correctly.\n\nQuestion: %s\n\nRubric: %s\n\n"+
				"Model Response: %s\n\nIs the model response correct? Answer yes or no only.",
			question, answer, response)
	}
	return fmt.Sprintf("Question: %s\n\nCorrect Answer: %s\n\nModel Response: %s\n\nIs the model response correct? Answer yes or no only.",
		question, answer, response)
}

func runLongMemEvalGrade(args []string) error {
	var hypPath string
	var dataPath string
	var graderModel string
	var concurrency int
	var verbose bool
	var resultsPath string
	fs, err := parseFlags("longmemeval-grade", args, func(fs *flag.FlagSet) {
		fs.StringVar(&hypPath, "hyp", "", "hypothesis JSONL produced by longmemeval-qa")
		fs.StringVar(&dataPath, "data", "", "path to longmemeval_s_cleaned.json (default: bench/.corpus/)")
		fs.StringVar(&graderModel, "grader", "openai/gpt-4o-mini-2024-07-18", "grader model (OpenRouter slug)")
		fs.IntVar(&concurrency, "concurrency", 8, "number of parallel grader calls")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write graded JSONL to this path (default: <hyp>.graded.jsonl)")
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

	resolved, err := resolveDataPath(dataPath, defaultLongMemEvalFile)
	if err != nil {
		return err
	}
	questions, err := loadLongMemEval(resolved)
	if err != nil {
		return err
	}
	qmap := map[string]lmeQuestion{}
	for _, q := range questions {
		qmap[q.QuestionID] = q
	}

	hyps, err := loadHypotheses(hypPath)
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
		h   answererHypothesis
	}
	jobs := make(chan job, concurrency*2)
	results := make([]graderRecord, len(hyps))
	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			q, ok := qmap[j.h.QuestionID]
			if !ok {
				results[j.idx] = graderRecord{answererHypothesis: j.h, AutoEvalLabel: autoEvalLabel{Model: graderModel}}
				continue
			}
			answerStr := answerToString(q.Answer)
			prompt := answerCheckPrompt(j.h.QuestionType, q.Question, answerStr, j.h.Hypothesis, j.h.Abstention)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := client.Complete(ctx, graderModel, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, 10)
			cancel()
			rec := graderRecord{answererHypothesis: j.h, AutoEvalLabel: autoEvalLabel{Model: graderModel}}
			if err != nil {
				rec.GraderRaw = "ERROR: " + err.Error()
			} else {
				rec.GraderRaw = strings.TrimSpace(res.Content)
				rec.GraderCost = res.CostUSD
				rec.AutoEvalLabel.Label = strings.Contains(strings.ToLower(rec.GraderRaw), "yes")
				costMu.Lock()
				totalCost += res.CostUSD
				costMu.Unlock()
			}
			results[j.idx] = rec
			n := atomic.AddInt32(&doneCount, 1)
			if int(n)%25 == 0 || int(n) == len(hyps) {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] grader cost=$%.4f\n", n, len(hyps), totalCost)
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s (%s): label=%v hyp=%q\n", j.h.QuestionID, j.h.QuestionType, rec.AutoEvalLabel.Label, truncString(j.h.Hypothesis, 80))
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
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}

	summary := aggregateGraded(results)
	summary.GraderModel = graderModel
	summary.GraderCostUSD = totalCost
	summary.HypothesisPath = hypPath
	summary.GradedPath = resultsPath

	summaryPath := strings.TrimSuffix(resultsPath, ".jsonl") + "_summary.json"
	if err := writeJSONFile(summaryPath, summary); err != nil {
		return err
	}
	mdPath := strings.TrimSuffix(resultsPath, ".jsonl") + ".md"
	if err := writeQAResultsMarkdown(mdPath, summary); err != nil {
		return err
	}

	printQASummary(os.Stdout, summary)
	fmt.Fprintf(os.Stderr, "\ngraded:   %s\nsummary:  %s\nmarkdown: %s\n", resultsPath, summaryPath, mdPath)
	fmt.Fprintf(os.Stderr, "grader cost: $%.4f\n", totalCost)
	return nil
}

func answerToString(v any) string {
	switch a := v.(type) {
	case string:
		return a
	case []any:
		parts := make([]string, 0, len(a))
		for _, x := range a {
			parts = append(parts, fmt.Sprintf("%v", x))
		}
		return strings.Join(parts, "; ")
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func loadHypotheses(path string) ([]answererHypothesis, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []answererHypothesis
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h answererHypothesis
		if err := json.Unmarshal([]byte(line), &h); err != nil {
			return nil, fmt.Errorf("parse line: %w", err)
		}
		out = append(out, h)
	}
	return out, sc.Err()
}

type qaCategorySummary struct {
	Count      int     `json:"count"`
	Correct    int     `json:"correct"`
	Accuracy   float64 `json:"accuracy"`
	Abstention bool    `json:"-"`
}

type qaSummary struct {
	HypothesisPath    string                       `json:"hypothesis_path"`
	GradedPath        string                       `json:"graded_path"`
	GraderModel       string                       `json:"grader_model"`
	GraderCostUSD     float64                      `json:"grader_cost_usd"`
	AnswererModel     string                       `json:"answerer_model"`
	RetrievalMode     string                       `json:"retrieval_mode"`
	TopK              int                          `json:"top_k"`
	Total             int                          `json:"total"`
	Scored            int                          `json:"scored"`
	AbstentionCount   int                          `json:"abstention_count"`
	OverallAccuracy   float64                      `json:"overall_accuracy"`
	IncludingAbstAcc  float64                      `json:"accuracy_incl_abstention"`
	ByCategory        map[string]qaCategorySummary `json:"by_category"`
	AnswererCostTotal float64                      `json:"answerer_cost_total"`
	AnswererLatencyP50 int64                       `json:"answerer_latency_p50_ms"`
	AnswererLatencyP95 int64                       `json:"answerer_latency_p95_ms"`
}

func aggregateGraded(recs []graderRecord) qaSummary {
	s := qaSummary{
		Total:      len(recs),
		ByCategory: map[string]qaCategorySummary{},
	}
	type bucket struct {
		count, correct int
	}
	cats := map[string]*bucket{}
	var absBucket bucket
	var allBucket bucket
	var latencies []int64
	for _, r := range recs {
		if r.AnswererModel != "" {
			s.AnswererModel = r.AnswererModel
		}
		if r.RetrievalMode != "" {
			s.RetrievalMode = r.RetrievalMode
		}
		if r.TopK > 0 {
			s.TopK = r.TopK
		}
		s.AnswererCostTotal += r.CostUSD
		if r.LatencyMS > 0 {
			latencies = append(latencies, r.LatencyMS)
		}
		allBucket.count++
		if r.AutoEvalLabel.Label {
			allBucket.correct++
		}
		if r.Abstention {
			s.AbstentionCount++
			absBucket.count++
			if r.AutoEvalLabel.Label {
				absBucket.correct++
			}
			continue
		}
		s.Scored++
		c, ok := cats[r.QuestionType]
		if !ok {
			c = &bucket{}
			cats[r.QuestionType] = c
		}
		c.count++
		if r.AutoEvalLabel.Label {
			c.correct++
		}
	}
	if s.Scored > 0 {
		total := 0
		for _, c := range cats {
			total += c.correct
		}
		s.OverallAccuracy = float64(total) / float64(s.Scored)
	}
	if allBucket.count > 0 {
		s.IncludingAbstAcc = float64(allBucket.correct) / float64(allBucket.count)
	}
	for k, c := range cats {
		s.ByCategory[k] = qaCategorySummary{
			Count:    c.count,
			Correct:  c.correct,
			Accuracy: safeDiv(c.correct, c.count),
		}
	}
	if absBucket.count > 0 {
		s.ByCategory["abstention"] = qaCategorySummary{
			Count:      absBucket.count,
			Correct:    absBucket.correct,
			Accuracy:   safeDiv(absBucket.correct, absBucket.count),
			Abstention: true,
		}
	}
	if n := len(latencies); n > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		s.AnswererLatencyP50 = latencies[n/2]
		idx95 := int(float64(n) * 0.95)
		if idx95 >= n {
			idx95 = n - 1
		}
		s.AnswererLatencyP95 = latencies[idx95]
	}
	return s
}

func printQASummary(w *os.File, s qaSummary) {
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "answerer: %s\n", s.AnswererModel)
	fmt.Fprintf(w, "grader: %s\n", s.GraderModel)
	fmt.Fprintf(w, "retrieval_mode: %s\n", s.RetrievalMode)
	fmt.Fprintf(w, "top_k: %d\n", s.TopK)
	fmt.Fprintf(w, "scored: %d (excluding %d abstention)\n", s.Scored, s.AbstentionCount)
	fmt.Fprintf(w, "overall_accuracy: %.4f\n", s.OverallAccuracy)
	fmt.Fprintf(w, "accuracy_incl_abstention: %.4f\n", s.IncludingAbstAcc)
	fmt.Fprintf(w, "answerer_cost_total: $%.4f\n", s.AnswererCostTotal)
	fmt.Fprintf(w, "answerer_latency_p50_ms: %d\n", s.AnswererLatencyP50)
	fmt.Fprintf(w, "answerer_latency_p95_ms: %d\n", s.AnswererLatencyP95)
	fmt.Fprintf(w, "---\n")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(w, "%-32s  n=%4d  acc=%.4f\n", k, c.Count, c.Accuracy)
	}
}

func writeQAResultsMarkdown(path string, s qaSummary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# LongMemEval QA — recoil\n\n")
	fmt.Fprintf(f, "- Answerer: `%s`\n", s.AnswererModel)
	fmt.Fprintf(f, "- Grader: `%s`\n", s.GraderModel)
	fmt.Fprintf(f, "- Retrieval mode: `%s`, top-K=%d\n", s.RetrievalMode, s.TopK)
	fmt.Fprintf(f, "- Questions: %d total, %d scored (abstention excluded), %d abstention\n",
		s.Total, s.Scored, s.AbstentionCount)
	fmt.Fprintf(f, "- Answerer cost: $%.4f, grader cost: $%.4f\n", s.AnswererCostTotal, s.GraderCostUSD)
	fmt.Fprintf(f, "- Answerer latency: p50=%dms, p95=%dms\n\n", s.AnswererLatencyP50, s.AnswererLatencyP95)
	fmt.Fprintf(f, "## Headline\n\n")
	fmt.Fprintf(f, "| Metric | Value |\n|---|---:|\n")
	fmt.Fprintf(f, "| Accuracy (excluding abstention) | **%.4f** |\n", s.OverallAccuracy)
	fmt.Fprintf(f, "| Accuracy (including abstention) | %.4f |\n\n", s.IncludingAbstAcc)
	fmt.Fprintf(f, "## By question type\n\n")
	fmt.Fprintf(f, "| Type | Count | Correct | Accuracy |\n|---|---:|---:|---:|\n")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(f, "| %s | %d | %d | %.4f |\n", k, c.Count, c.Correct, c.Accuracy)
	}
	return nil
}

var _ = filepath.Base
