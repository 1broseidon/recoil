package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

// locomoHypothesis is one per-question answerer record (mirrors the
// LongMemEval qa schema where possible so the grader can be uniform).
type locomoHypothesis struct {
	SampleID      string  `json:"sample_id"`
	QuestionIndex int     `json:"question_index"`
	Category      int     `json:"category"`
	CategoryName  string  `json:"category_name"`
	Question      string  `json:"question"`
	GoldAnswer    string  `json:"gold_answer,omitempty"`
	GoldRubric    string  `json:"gold_rubric,omitempty"`
	Adversarial   bool    `json:"adversarial,omitempty"`
	Hypothesis    string  `json:"hypothesis"`
	RetrievalMode string  `json:"retrieval_mode"`
	AnswererModel string  `json:"answerer_model"`
	TopK          int     `json:"top_k"`
	PromptTokens  int     `json:"prompt_tokens"`
	CompletionTok int     `json:"completion_tokens"`
	CostUSD       float64 `json:"cost_usd"`
	LatencyMS     int64   `json:"latency_ms"`
	Error         string  `json:"error,omitempty"`
}

func runLoCoMoQA(args []string) error {
	var dataPath, model, modeStr, resultsPath, reasoningEffort string
	var topK, limit, concurrency, maxTokens int
	var verbose, hybridEmbedding bool
	var embedProvider, embedModel, ollamaHost string
	var maxEmbedChars, chunkChars, chunkOverlap, fusionK, ftsPool, embedPool int
	fs, err := parseFlags("locomo-qa", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to locomo10.json (default: bench/.corpus/)")
		fs.StringVar(&model, "answerer", "openai/gpt-4o-mini-2024-07-18", "answerer model (OpenRouter slug)")
		fs.StringVar(&modeStr, "mode", "recoil-k10", "retrieval mode: recoil-k5 | recoil-k10 | recoil-k20 | recoil-k30")
		fs.IntVar(&topK, "top-k", 0, "override top-k (default derived from --mode)")
		fs.IntVar(&limit, "limit", 0, "only run the first N records (0 = all 10)")
		fs.IntVar(&concurrency, "concurrency", 8, "parallel LLM calls")
		fs.IntVar(&maxTokens, "max-tokens", 2000, "answerer max_tokens")
		fs.StringVar(&reasoningEffort, "reasoning-effort", "", "reasoning effort: minimal | low | medium | high")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write hypothesis JSONL")
		fs.BoolVar(&hybridEmbedding, "hybrid-embedding", false, "rerank FTS candidates with cosine over embeddings (RRF fusion)")
		fs.StringVar(&embedProvider, "embed-provider", "openrouter", "openrouter | ollama")
		fs.StringVar(&embedModel, "embed-model", "", "embedding model (default depends on provider)")
		fs.StringVar(&ollamaHost, "ollama-host", "", "Ollama base URL")
		fs.IntVar(&maxEmbedChars, "max-embed-chars", 0, "truncate embedding inputs to this many chars")
		fs.IntVar(&chunkChars, "chunk-chars", 0, "chunk each row into windows of this size (max-pool cosine over chunks). 0 = no chunking")
		fs.IntVar(&chunkOverlap, "chunk-overlap", 500, "overlap between adjacent chunks")
		fs.IntVar(&fusionK, "fusion-k", 60, "RRF fusion constant")
		fs.IntVar(&ftsPool, "fts-pool", 50, "FTS candidate pool size before fusion")
		fs.IntVar(&embedPool, "embed-pool", 50, "embedding candidate pool")
	})
	if err != nil {
		return err
	}
	_ = fs
	mode := retrievalMode(modeStr)
	if topK == 0 {
		topK = defaultTopK(mode)
		if topK == 0 {
			topK = 10
		}
	}

	var hc *hybridConfig
	if hybridEmbedding {
		hc, err = buildHybridConfig(embedProvider, embedModel, ollamaHost, maxEmbedChars, chunkChars, chunkOverlap, fusionK, ftsPool, embedPool)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "hybrid retrieval: provider=%s model=%s fts-pool=%d embed-pool=%d\n",
			hc.embedProvider, hc.embedModel, hc.ftsPoolSize, hc.embedPoolSize)
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	resolved, err := resolveDataPath(dataPath, defaultLoCoMoFile)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "loading %s ...\n", resolved)
	records, err := loadLoCoMo(resolved)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}

	// Pre-build (record, qa, ingested turns) tuples. We need the turn map keyed
	// by dia_id so we can rebuild context for each question after retrieval.
	preps := make([]*locomoPrepared, 0, len(records))
	for ri, rec := range records {
		sessions, err := parseConversation(rec.Conversation)
		if err != nil {
			return fmt.Errorf("record %d: %w", ri, err)
		}
		tmpDir, err := os.MkdirTemp("", "recoil-locomo-qa-")
		if err != nil {
			return err
		}
		dbPath := filepath.Join(tmpDir, "qa.db")
		st, err := store.Open(dbPath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return err
		}
		scopeID := "locomo-qa:" + rec.SampleID
		ctx := context.Background()
		turnByDiaID := map[string]locomoTurn{}
		for _, sess := range sessions {
			for _, turn := range sess.Turns {
				text := strings.TrimSpace(turn.Text)
				if text == "" {
					continue
				}
				turnByDiaID[turn.DiaID] = turn
				_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
					Role:       "source",
					Content:    turn.Speaker + ": " + text,
					SourceKind: "session",
					SourcePath: "session/" + sess.ID,
					SourceRef:  turn.DiaID,
					ScopeKind:  "project",
					ScopeID:    scopeID,
					Validity:   "unknown",
					MetadataJSON: fmt.Sprintf(`{"session_date":%q,"speaker":%q}`, sess.Date, turn.Speaker),
				})
				if err != nil {
					st.Close()
					os.RemoveAll(tmpDir)
					return err
				}
			}
		}
		preps = append(preps, &locomoPrepared{rec: rec, turnByDiaID: turnByDiaID, st: st, tmpDir: tmpDir, scopeID: scopeID})
	}
	defer func() {
		for _, p := range preps {
			p.st.Close()
			os.RemoveAll(p.tmpDir)
		}
	}()

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		modelSlug := strings.ReplaceAll(strings.ReplaceAll(model, "/", "_"), ".", "")
		resultsPath = filepath.Join(outDir, fmt.Sprintf("locomo_qa_%s_%s_%s.jsonl", mode, modelSlug, runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()

	totalQ := 0
	for _, p := range preps {
		totalQ += len(p.rec.QA)
	}
	fmt.Fprintf(os.Stderr, "loaded %d records, %d questions; mode=%s top-k=%d answerer=%s concurrency=%d\n",
		len(preps), totalQ, mode, topK, model, concurrency)

	type job struct {
		idx    int
		prep   *locomoPrepared
		qa     locomoQA
		qIndex int
	}
	jobs := make(chan job, concurrency*2)
	results := make([]locomoHypothesis, totalQ)
	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			h := scoreLoCoMoQA(j.prep, j.qa, j.qIndex, mode, topK, model, client, maxTokens, reasoningEffort, hc)
			results[j.idx] = h
			costMu.Lock()
			totalCost += h.CostUSD
			costMu.Unlock()
			n := atomic.AddInt32(&doneCount, 1)
			if int(n)%25 == 0 || int(n) == totalQ {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] cost=$%.4f  last cat=%d err=%q\n",
					n, totalQ, totalCost, j.qa.Category, truncString(h.Error, 60))
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s/cat%d: hyp=%q\n", j.prep.rec.SampleID, j.qa.Category, truncString(h.Hypothesis, 100))
			}
		}
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker()
	}
	idx := 0
	for _, p := range preps {
		for qi, qa := range p.rec.QA {
			jobs <- job{idx: idx, prep: p, qa: qa, qIndex: qi}
			idx++
		}
	}
	close(jobs)
	wg.Wait()

	enc := json.NewEncoder(resultsFile)
	for _, h := range results {
		if err := enc.Encode(h); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\ntotal answerer cost: $%.4f\nnext: go run ./bench locomo-grade --hyp %s\n",
		resultsPath, totalCost, resultsPath)
	return nil
}

type locomoPrepared struct {
	rec         locomoRecord
	turnByDiaID map[string]locomoTurn
	st          *store.Store
	tmpDir      string
	scopeID     string
}

func scoreLoCoMoQA(p *locomoPrepared, qa locomoQA, qIndex int, mode retrievalMode, topK int, model string, client *OpenRouterClient, maxTokens int, reasoningEffort string, hc *hybridConfig) locomoHypothesis {
	h := locomoHypothesis{
		SampleID:      p.rec.SampleID,
		QuestionIndex: qIndex,
		Category:      qa.Category,
		CategoryName:  locomoCategoryName(qa.Category),
		Question:      qa.Question,
		GoldAnswer:    locomoGoldAnswer(qa),
		Adversarial:   qa.Category == 5,
		RetrievalMode: string(mode),
		AnswererModel: model,
		TopK:          topK,
	}
	ctx := context.Background()
	// For hybrid we want a bigger FTS pool to fuse over; otherwise grab topK.
	ftsLimit := topK
	if hc != nil {
		if hc.ftsPoolSize > ftsLimit {
			ftsLimit = hc.ftsPoolSize
		}
		if ftsLimit > 100 {
			ftsLimit = 100
		}
	}
	rows, err := p.st.Search(ctx, store.SearchParams{
		Query:        qa.Question,
		ScopeKind:    "project",
		ScopeID:      p.scopeID,
		Limit:        ftsLimit,
		Lifecycle:    store.LifecycleAny,
		SignalRerank: true,
	})
	if err != nil {
		h.Error = "retrieval: " + err.Error()
		return h
	}
	if hc != nil && len(rows) > 1 {
		ctxEmb, cancelEmb := context.WithTimeout(ctx, 120*time.Second)
		reranked, rerr := hybridRerankCandidates(ctxEmb, hc, qa.Question, rows, topK)
		cancelEmb()
		if rerr == nil {
			rows = reranked
		} else if h.Error == "" {
			h.Error = "hybrid_warn: " + rerr.Error()
			// keep the FTS-only rows
			if len(rows) > topK {
				rows = rows[:topK]
			}
		}
	} else if len(rows) > topK {
		rows = rows[:topK]
	}

	prompt := buildLoCoMoPrompt(qa, rows)
	cctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	t0 := time.Now()
	res, err := client.CompleteWithOptions(cctx, model, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, maxTokens, CompleteOptions{ReasoningEffort: reasoningEffort})
	h.LatencyMS = time.Since(t0).Milliseconds()
	if err != nil {
		h.Error = err.Error()
		return h
	}
	if strings.TrimSpace(res.Content) == "" && res.CompletionTok >= maxTokens-1 {
		res2, err2 := client.CompleteWithOptions(cctx, model, []ChatMessage{{Role: "user", Content: prompt}}, 0.0, maxTokens*2, CompleteOptions{ReasoningEffort: reasoningEffort})
		if err2 == nil && strings.TrimSpace(res2.Content) != "" {
			res = res2
		}
	}
	h.Hypothesis = strings.TrimSpace(res.Content)
	h.PromptTokens = res.PromptTok
	h.CompletionTok = res.CompletionTok
	h.CostUSD = res.CostUSD
	if h.Hypothesis == "" {
		h.Error = fmt.Sprintf("empty content (completion_tokens=%d, max_tokens=%d)", res.CompletionTok, maxTokens)
	}
	return h
}

// buildLoCoMoPrompt formats the retrieved turns into a prompt mirroring the
// LongMemEval Chain-of-Note recipe but turn-grained.
func buildLoCoMoPrompt(qa locomoQA, rows []store.Memory) string {
	var b strings.Builder
	b.WriteString("I will give you several turns from a long-running conversation between two people. ")
	b.WriteString("Please answer the question based on the relevant turns. ")
	b.WriteString("Answer step by step: first extract relevant facts, then reason to the answer.\n\n")
	b.WriteString("Retrieved turns:\n")
	for i, m := range rows {
		fmt.Fprintf(&b, "\n### Turn %d (dia_id=%s, session=%s):\n%s\n",
			i+1, m.SourceRef, strings.TrimPrefix(m.SourcePath, "session/"), m.Content)
	}
	if qa.Category == 5 {
		fmt.Fprintf(&b, "\nQuestion: %s\nIf the retrieved turns do not contain the answer, respond \"I don't know\" or \"the information is not available.\"\nAnswer (step by step):", qa.Question)
	} else {
		fmt.Fprintf(&b, "\nQuestion: %s\nAnswer (step by step):", qa.Question)
	}
	return b.String()
}

func locomoGoldAnswer(qa locomoQA) string {
	if qa.Answer != nil {
		return anyToString(qa.Answer)
	}
	if strings.TrimSpace(qa.AdversarialAnswer) != "" {
		return qa.AdversarialAnswer
	}
	return ""
}

func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, anyToString(e))
		}
		return strings.Join(parts, "; ")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
