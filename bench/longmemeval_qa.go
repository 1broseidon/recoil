package main

import (
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

	"github.com/1broseidon/recoil/internal/store"
)

// retrievalMode describes how the answerer's context window gets populated.
type retrievalMode string

const (
	modeRecoilK5    retrievalMode = "recoil-k5"
	modeRecoilK10   retrievalMode = "recoil-k10"
	modeRecoilK20   retrievalMode = "recoil-k20"
	modeRecoilK30   retrievalMode = "recoil-k30"
	modeNoRetrieval retrievalMode = "no-retrieval"
	modeOracle      retrievalMode = "oracle"
	modeFullContext retrievalMode = "full-context"
)

// answererHypothesis is the per-question record we persist. Field names match
// LongMemEval's evaluate_qa.py expectations (question_id, hypothesis) so the
// official Python grader can consume our output unchanged if we ever want to.
type answererHypothesis struct {
	QuestionID    string  `json:"question_id"`
	QuestionType  string  `json:"question_type"`
	Hypothesis    string  `json:"hypothesis"`
	RetrievalMode string  `json:"retrieval_mode"`
	AnswererModel string  `json:"answerer_model"`
	TopK          int     `json:"top_k"`
	PromptTokens  int     `json:"prompt_tokens"`
	CompletionTok int     `json:"completion_tokens"`
	CostUSD       float64 `json:"cost_usd"`
	LatencyMS     int64   `json:"latency_ms"`
	Abstention    bool    `json:"abstention"`
	Error         string  `json:"error,omitempty"`
}

func runLongMemEvalQA(args []string) error {
	var dataPath string
	var model string
	var modeStr string
	var topK int
	var limit int
	var concurrency int
	var maxTokens int
	var reasoningEffort string
	var verbose bool
	var resultsPath string
	fs, err := parseFlags("longmemeval-qa", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to longmemeval_s_cleaned.json (default: bench/.corpus/)")
		fs.StringVar(&model, "answerer", "openai/gpt-4o-mini-2024-07-18", "answerer model (OpenRouter slug)")
		fs.StringVar(&modeStr, "mode", string(modeRecoilK5), "retrieval mode: recoil-k5 | recoil-k10 | no-retrieval | oracle | full-context")
		fs.IntVar(&topK, "top-k", 0, "override top-k (default: derived from --mode)")
		fs.IntVar(&limit, "limit", 0, "only run the first N questions (0 = all)")
		fs.IntVar(&concurrency, "concurrency", 8, "number of parallel LLM calls")
		fs.IntVar(&maxTokens, "max-tokens", 2000, "answerer max_tokens (reasoning models need room for hidden CoT)")
		fs.StringVar(&reasoningEffort, "reasoning-effort", "", "reasoning model effort: minimal | low | medium | high (empty = provider default)")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write hypothesis JSONL to this path (default: bench/results/...)")
	})
	if err != nil {
		return err
	}
	_ = fs
	mode := retrievalMode(modeStr)
	if !isValidMode(mode) {
		return fmt.Errorf("unknown --mode %q", modeStr)
	}
	if topK == 0 {
		topK = defaultTopK(mode)
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	resolved, err := resolveDataPath(dataPath, defaultLongMemEvalFile)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "loading %s ...\n", resolved)
	questions, err := loadLongMemEval(resolved)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(questions) {
		questions = questions[:limit]
	}
	fmt.Fprintf(os.Stderr, "loaded %d questions, mode=%s top-k=%d answerer=%s concurrency=%d\n",
		len(questions), mode, topK, model, concurrency)

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		modelSlug := strings.ReplaceAll(strings.ReplaceAll(model, "/", "_"), ".", "")
		resultsPath = filepath.Join(outDir, fmt.Sprintf("qa_%s_%s_%s.jsonl", mode, modelSlug, runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()

	type job struct {
		idx int
		q   lmeQuestion
	}
	jobs := make(chan job, concurrency*2)
	results := make([]answererHypothesis, len(questions))
	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			h := scoreQuestionQA(j.q, mode, topK, model, client, maxTokens, reasoningEffort)
			results[j.idx] = h
			costMu.Lock()
			totalCost += h.CostUSD
			costMu.Unlock()
			n := atomic.AddInt32(&doneCount, 1)
			if int(n)%10 == 0 || int(n) == len(questions) {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] cost=$%.4f  last=%s err=%q\n",
					n, len(questions), totalCost, j.q.QuestionType, truncString(h.Error, 60))
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s (%s): hyp=%q\n", j.q.QuestionID, j.q.QuestionType, truncString(h.Hypothesis, 100))
			}
		}
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker()
	}
	for i, q := range questions {
		jobs <- job{idx: i, q: q}
	}
	close(jobs)
	wg.Wait()

	enc := json.NewEncoder(resultsFile)
	for _, h := range results {
		if err := enc.Encode(h); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", resultsPath)
	fmt.Fprintf(os.Stderr, "total answerer cost: $%.4f\n", totalCost)
	fmt.Fprintf(os.Stderr, "next: go run ./bench longmemeval-grade --hyp %s\n", resultsPath)
	return nil
}

func isValidMode(m retrievalMode) bool {
	switch m {
	case modeRecoilK5, modeRecoilK10, modeRecoilK20, modeRecoilK30, modeNoRetrieval, modeOracle, modeFullContext:
		return true
	}
	return false
}

func defaultTopK(m retrievalMode) int {
	switch m {
	case modeRecoilK5:
		return 5
	case modeRecoilK10:
		return 10
	case modeRecoilK20:
		return 20
	case modeRecoilK30:
		return 30
	case modeNoRetrieval:
		return 0
	case modeOracle:
		return 50 // bounded; oracle usually has <=3 sessions
	case modeFullContext:
		return 1000 // effectively all sessions
	}
	return 5
}

func scoreQuestionQA(q lmeQuestion, mode retrievalMode, topK int, model string, client *OpenRouterClient, maxTokens int, reasoningEffort string) answererHypothesis {
	h := answererHypothesis{
		QuestionID:    q.QuestionID,
		QuestionType:  q.QuestionType,
		RetrievalMode: string(mode),
		AnswererModel: model,
		TopK:          topK,
		Abstention:    strings.HasSuffix(q.QuestionID, "_abs"),
	}

	contextSessions, err := assembleContext(q, mode, topK)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	prompt := buildAnswererPrompt(q, contextSessions, mode)

	// Reasoning models can exhaust max_tokens entirely on hidden CoT and return
	// content=null. We retry once with doubled budget if that happens.
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	t0 := time.Now()
	res, err := client.CompleteWithOptions(ctx, model, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, maxTokens, CompleteOptions{ReasoningEffort: reasoningEffort})
	h.LatencyMS = time.Since(t0).Milliseconds()
	if err != nil {
		h.Error = err.Error()
		return h
	}
	if strings.TrimSpace(res.Content) == "" && res.CompletionTok >= maxTokens-1 {
		// Retry with double budget once.
		res2, err2 := client.CompleteWithOptions(ctx, model, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, maxTokens*2, CompleteOptions{ReasoningEffort: reasoningEffort})
		if err2 == nil && strings.TrimSpace(res2.Content) != "" {
			res = res2
		}
	}
	h.Hypothesis = strings.TrimSpace(res.Content)
	h.PromptTokens = res.PromptTok
	h.CompletionTok = res.CompletionTok
	h.CostUSD = res.CostUSD
	if h.Hypothesis == "" {
		h.Error = fmt.Sprintf("empty content (completion_tokens=%d, max_tokens=%d, likely reasoning exhausted budget)", res.CompletionTok, maxTokens)
	}
	return h
}

// contextSession is one item that lands in the answerer's prompt.
type contextSession struct {
	Date  string    `json:"date"`
	Turns []lmeTurn `json:"turns"`
}

// assembleContext implements each retrieval mode. For the recoil modes it does
// the same per-question store + Store.Search dance the retrieval harness uses;
// we don't share state with that harness because each question needs its own DB.
func assembleContext(q lmeQuestion, mode retrievalMode, topK int) ([]contextSession, error) {
	switch mode {
	case modeNoRetrieval:
		return nil, nil

	case modeFullContext:
		out := make([]contextSession, 0, len(q.HaystackSessions))
		for i, sess := range q.HaystackSessions {
			date := ""
			if i < len(q.HaystackDates) {
				date = q.HaystackDates[i]
			}
			out = append(out, contextSession{Date: date, Turns: sess})
		}
		return out, nil

	case modeOracle:
		ids := map[string]bool{}
		for _, sid := range q.AnswerSessionIDs {
			ids[sid] = true
		}
		out := make([]contextSession, 0, len(q.AnswerSessionIDs))
		for i, sid := range q.HaystackSessionIDs {
			if !ids[sid] || i >= len(q.HaystackSessions) {
				continue
			}
			date := ""
			if i < len(q.HaystackDates) {
				date = q.HaystackDates[i]
			}
			out = append(out, contextSession{Date: date, Turns: q.HaystackSessions[i]})
		}
		return out, nil

	case modeRecoilK5, modeRecoilK10, modeRecoilK20, modeRecoilK30:
		return assembleRecoilContext(q, topK)
	}
	return nil, fmt.Errorf("unhandled mode %s", mode)
}

func assembleRecoilContext(q lmeQuestion, topK int) ([]contextSession, error) {
	tmpDir, err := os.MkdirTemp("", "recoil-qa-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "qa.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	ctx := context.Background()
	scopeID := "lme-qa:" + q.QuestionID
	sidToIdx := map[string]int{}
	for i, sid := range q.HaystackSessionIDs {
		if i >= len(q.HaystackSessions) {
			break
		}
		sidToIdx[sid] = i
		content := formatSession(q.HaystackSessions[i])
		if strings.TrimSpace(content) == "" {
			continue
		}
		date := ""
		if i < len(q.HaystackDates) {
			date = q.HaystackDates[i]
		}
		_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
			Role:       "source",
			Content:    content,
			SourceKind: "session",
			SourcePath: sessionPathPrefix + sid,
			SourceRef:  date,
			ScopeKind:  "project",
			ScopeID:    scopeID,
			Validity:   "unknown",
		})
		if err != nil {
			return nil, err
		}
	}
	rows, err := st.Search(ctx, store.SearchParams{
		Query:     q.Question,
		ScopeKind: "project",
		ScopeID:   scopeID,
		Limit:     topK,
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		return nil, err
	}
	out := make([]contextSession, 0, len(rows))
	for _, mem := range rows {
		sid := strings.TrimPrefix(mem.SourcePath, sessionPathPrefix)
		idx, ok := sidToIdx[sid]
		if !ok {
			continue
		}
		date := ""
		if idx < len(q.HaystackDates) {
			date = q.HaystackDates[idx]
		}
		out = append(out, contextSession{Date: date, Turns: q.HaystackSessions[idx]})
	}
	return out, nil
}

// buildAnswererPrompt matches the LongMemEval paper's recommended template:
// history_format=json, useronly=false, reading_method=con (Chain-of-Note).
// Sessions are sorted by date before being shown so the model sees a coherent
// timeline. The "Answer step by step" instruction is the CoN prompt from
// src/generation/run_generation.py:55 in the official repo.
func buildAnswererPrompt(q lmeQuestion, sessions []contextSession, mode retrievalMode) string {
	// Strip has_answer markers; they are ground-truth labels, not context.
	cleaned := make([]contextSession, len(sessions))
	for i, s := range sessions {
		turns := make([]lmeTurn, len(s.Turns))
		for j, t := range s.Turns {
			t.HasAnswer = false
			turns[j] = t
		}
		cleaned[i] = contextSession{Date: s.Date, Turns: turns}
	}
	sort.SliceStable(cleaned, func(i, j int) bool { return cleaned[i].Date < cleaned[j].Date })

	if mode == modeNoRetrieval {
		return fmt.Sprintf("%sAnswer step by step.", q.Question+"\n\n")
	}

	var b strings.Builder
	b.WriteString("I will give you several history chats between you and a user. ")
	b.WriteString("Please answer the question based on the relevant chat history. ")
	b.WriteString("Answer the question step by step: first extract all the relevant information, ")
	b.WriteString("and then reason over the information to get the answer.\n\n\nHistory Chats:\n")
	for i, s := range cleaned {
		// Marshal turns as JSON, matching history_format=json from the paper.
		raw, _ := json.Marshal(s.Turns)
		fmt.Fprintf(&b, "\n### Session %d:\nSession Date: %s\nSession Content:\n%s\n", i+1, s.Date, string(raw))
	}
	fmt.Fprintf(&b, "\nCurrent Date: %s\nQuestion: %s\nAnswer (step by step):", q.QuestionDate, q.Question)
	return b.String()
}

func truncString(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
