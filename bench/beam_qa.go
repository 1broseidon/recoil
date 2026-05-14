package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

type beamHypothesis struct {
	ConversationID string  `json:"conversation_id"`
	Scale          string  `json:"scale"`
	Category       string  `json:"category"`
	Question       string  `json:"question"`
	IdealResponse  string  `json:"ideal_response,omitempty"`
	IdealAnswer    string  `json:"ideal_answer,omitempty"`
	Rubric         []any   `json:"rubric,omitempty"`
	Abstention     bool    `json:"abstention,omitempty"`
	Hypothesis     string  `json:"hypothesis"`
	RetrievalMode  string  `json:"retrieval_mode"`
	AnswererModel  string  `json:"answerer_model"`
	TopK           int     `json:"top_k"`
	PromptTokens   int     `json:"prompt_tokens"`
	CompletionTok  int     `json:"completion_tokens"`
	CostUSD        float64 `json:"cost_usd"`
	LatencyMS      int64   `json:"latency_ms"`
	Error          string  `json:"error,omitempty"`
}

func runBEAMQA(args []string) error {
	var dataDir, scale, model, modeStr, resultsPath, reasoningEffort string
	var topK, limit, concurrency, maxTokens int
	var verbose, hybridEmbedding bool
	var embedProvider, embedModel, ollamaHost string
	var maxEmbedChars, chunkChars, chunkOverlap, fusionK, ftsPool, embedPool int
	fs, err := parseFlags("beam-qa", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataDir, "data", "", "path to bench/.corpus/beam/<scale>/ (default: derived from --scale)")
		fs.StringVar(&scale, "scale", defaultBEAMScale, "BEAM scale: 100K | 500K | 1M | 10M")
		fs.StringVar(&model, "answerer", "openai/gpt-4o-mini-2024-07-18", "answerer model")
		fs.StringVar(&modeStr, "mode", "recoil-k10", "retrieval mode (recoil-k5/10/20/30)")
		fs.IntVar(&topK, "top-k", 0, "override top-k")
		fs.IntVar(&limit, "limit", 0, "only run the first N conversations")
		fs.IntVar(&concurrency, "concurrency", 8, "parallel LLM calls")
		fs.IntVar(&maxTokens, "max-tokens", 2000, "answerer max_tokens")
		fs.StringVar(&reasoningEffort, "reasoning-effort", "", "reasoning effort: minimal | low | medium | high")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write hypothesis JSONL")
		fs.BoolVar(&hybridEmbedding, "hybrid-embedding", false, "rerank FTS candidates with cosine over embeddings (RRF fusion)")
		fs.StringVar(&embedProvider, "embed-provider", "openrouter", "openrouter | ollama")
		fs.StringVar(&embedModel, "embed-model", "", "embedding model")
		fs.StringVar(&ollamaHost, "ollama-host", "", "Ollama base URL")
		fs.IntVar(&maxEmbedChars, "max-embed-chars", 0, "truncate embedding inputs to this many chars")
		fs.IntVar(&chunkChars, "chunk-chars", 0, "chunk each row into windows of this size")
		fs.IntVar(&chunkOverlap, "chunk-overlap", 500, "chunk overlap")
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

	if dataDir == "" {
		root, err := corpusRoot()
		if err != nil {
			return err
		}
		dataDir = filepath.Join(root, "beam", scale)
	}
	if info, err := os.Stat(dataDir); err != nil || !info.IsDir() {
		return fmt.Errorf("BEAM corpus not found at %s — run `bash bench/fetch_beam.sh %s` first", dataDir, scale)
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	convDirs, err := listConvDirs(dataDir)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(convDirs) {
		convDirs = convDirs[:limit]
	}
	fmt.Fprintf(os.Stderr, "BEAM-QA scale=%s convs=%d mode=%s top-k=%d answerer=%s\n",
		scale, len(convDirs), mode, topK, model)

	// Open one store per conversation, keep alive across all its probing questions.
	type prepared struct {
		convID  string
		st      *store.Store
		tmpDir  string
		scopeID string
		probing beamProbing
	}
	preps := make([]*prepared, 0, len(convDirs))
	defer func() {
		for _, p := range preps {
			if p.st != nil {
				p.st.Close()
			}
			if p.tmpDir != "" {
				os.RemoveAll(p.tmpDir)
			}
		}
	}()
	for ci, convDir := range convDirs {
		convID := filepath.Base(convDir)
		batches, err := loadBEAMChat(filepath.Join(convDir, "chat.json"))
		if err != nil {
			return fmt.Errorf("conv %s: %w", convID, err)
		}
		probing, err := loadBEAMProbing(filepath.Join(convDir, "probing_questions", "probing_questions.json"))
		if err != nil {
			return fmt.Errorf("conv %s: %w", convID, err)
		}
		tmpDir, err := os.MkdirTemp("", "recoil-beam-qa-")
		if err != nil {
			return err
		}
		dbPath := filepath.Join(tmpDir, "qa.db")
		st, err := store.Open(dbPath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return err
		}
		scopeID := "beam-qa:" + scale + ":" + convID
		ctx := context.Background()
		for _, batch := range batches {
			for _, tg := range batch.Turns {
				for _, turn := range tg {
					text := strings.TrimSpace(turn.Content)
					if text == "" {
						continue
					}
					_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
						Role:       "source",
						Content:    turn.Role + ": " + text,
						SourceKind: "session",
						SourcePath: fmt.Sprintf("batch/%d", batch.BatchNumber),
						SourceRef:  strconv.Itoa(turn.ID),
						ScopeKind:  "project",
						ScopeID:    scopeID,
						Validity:   "unknown",
						MetadataJSON: fmt.Sprintf(`{"time_anchor":%q,"question_type":%q}`,
							firstNonEmpty(turn.TimeAnchor, batch.TimeAnchor), turn.QuestionType),
					})
					if err != nil {
						return fmt.Errorf("ingest conv %s turn %d: %w", convID, turn.ID, err)
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, "[ingest %d/%d] %s\n", ci+1, len(convDirs), convID)
		preps = append(preps, &prepared{convID: convID, st: st, tmpDir: tmpDir, scopeID: scopeID, probing: probing})
	}

	// Job queue.
	type job struct {
		idx     int
		prep    *prepared
		cat     string
		qa      beamQuestion
	}
	jobs := make(chan job, concurrency*2)
	var allJobs []job
	idx := 0
	for _, p := range preps {
		cats := make([]string, 0, len(p.probing))
		for k := range p.probing {
			cats = append(cats, k)
		}
		sort.Strings(cats)
		for _, cat := range cats {
			for _, qa := range p.probing[cat] {
				allJobs = append(allJobs, job{idx: idx, prep: p, cat: cat, qa: qa})
				idx++
			}
		}
	}
	results := make([]beamHypothesis, len(allJobs))

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		modelSlug := strings.ReplaceAll(strings.ReplaceAll(model, "/", "_"), ".", "")
		resultsPath = filepath.Join(outDir, fmt.Sprintf("beam_qa_%s_%s_%s_%s.jsonl", strings.ToLower(scale), mode, modelSlug, runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()

	var wg sync.WaitGroup
	var doneCount int32
	var totalCost float64
	var costMu sync.Mutex
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			h := scoreBEAMQA(j.prep.st, j.prep.scopeID, j.prep.convID, scale, j.cat, j.qa, mode, topK, model, client, maxTokens, reasoningEffort, hc)
			results[j.idx] = h
			costMu.Lock()
			totalCost += h.CostUSD
			costMu.Unlock()
			n := atomic.AddInt32(&doneCount, 1)
			if int(n)%25 == 0 || int(n) == len(allJobs) {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] cost=$%.4f  last %s/%s err=%q\n",
					n, len(allJobs), totalCost, j.cat, j.prep.convID, truncString(h.Error, 60))
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s/%s: hyp=%q\n", j.cat, j.prep.convID, truncString(h.Hypothesis, 100))
			}
		}
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker()
	}
	for _, j := range allJobs {
		jobs <- j
	}
	close(jobs)
	wg.Wait()

	enc := json.NewEncoder(resultsFile)
	for _, h := range results {
		if err := enc.Encode(h); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s\ntotal answerer cost: $%.4f\nnext: go run ./bench beam-grade --hyp %s\n",
		resultsPath, totalCost, resultsPath)
	return nil
}

func scoreBEAMQA(st *store.Store, scopeID, convID, scale, cat string, qa beamQuestion, mode retrievalMode, topK int, model string, client *OpenRouterClient, maxTokens int, reasoningEffort string, hc *hybridConfig) beamHypothesis {
	h := beamHypothesis{
		ConversationID: convID,
		Scale:          scale,
		Category:       cat,
		Question:       qa.Question,
		IdealResponse:  qa.IdealResponse,
		IdealAnswer:    qa.IdealAnswer,
		Abstention:     cat == "abstention",
		RetrievalMode:  string(mode),
		AnswererModel:  model,
		TopK:           topK,
	}
	ctx := context.Background()
	ftsLimit := topK
	if hc != nil {
		if hc.ftsPoolSize > ftsLimit {
			ftsLimit = hc.ftsPoolSize
		}
		if ftsLimit > 100 {
			ftsLimit = 100
		}
	}
	rows, err := st.Search(ctx, store.SearchParams{
		Query:        qa.Question,
		ScopeKind:    "project",
		ScopeID:      scopeID,
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
			if len(rows) > topK {
				rows = rows[:topK]
			}
		}
	} else if len(rows) > topK {
		rows = rows[:topK]
	}

	prompt := buildBEAMPrompt(cat, qa, rows)
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

func buildBEAMPrompt(cat string, qa beamQuestion, rows []store.Memory) string {
	var b strings.Builder
	b.WriteString("I will give you several retrieved turns from a long-running conversation. ")
	b.WriteString("Please answer the question based on the retrieved context. ")
	b.WriteString("Answer step by step: first extract the relevant facts, then reason to the answer.\n\n")
	b.WriteString("Retrieved turns:\n")
	for i, m := range rows {
		fmt.Fprintf(&b, "\n### Turn %d (chat_id=%s):\n%s\n", i+1, m.SourceRef, m.Content)
	}
	switch cat {
	case "abstention":
		fmt.Fprintf(&b, "\nQuestion: %s\nIf the retrieved turns do not contain the answer, respond that the information is unavailable.\nAnswer (step by step):", qa.Question)
	case "contradiction_resolution":
		fmt.Fprintf(&b, "\nQuestion: %s\nIf the retrieved turns contain contradictory statements, state the contradiction and ask for clarification rather than choosing one.\nAnswer (step by step):", qa.Question)
	default:
		fmt.Fprintf(&b, "\nQuestion: %s\nAnswer (step by step):", qa.Question)
	}
	return b.String()
}
