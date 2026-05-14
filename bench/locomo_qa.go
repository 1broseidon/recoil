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
	var dataPath, model, modeStr, resultsPath, reasoningEffort, factsPath, profilesPath string
	var topK, limit, concurrency, maxTokens, maxQuestions, factsTopK, profilesTopK int
	var verbose, hybridEmbedding bool
	var embedProvider, embedModel, ollamaHost string
	var maxEmbedChars, chunkChars, chunkOverlap, fusionK, ftsPool, embedPool int
	fs, err := parseFlags("locomo-qa", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to locomo10.json (default: bench/.corpus/)")
		fs.StringVar(&model, "answerer", "openai/gpt-4o-mini-2024-07-18", "answerer model (OpenRouter slug)")
		fs.StringVar(&modeStr, "mode", "recoil-k10", "retrieval mode: recoil-k5 | recoil-k10 | recoil-k20 | recoil-k30")
		fs.IntVar(&topK, "top-k", 0, "override top-k (default derived from --mode)")
		fs.IntVar(&limit, "limit", 0, "only run the first N records (0 = all 10)")
		fs.IntVar(&maxQuestions, "max-questions", 0, "global cap on total questions to score (0 = no cap). Useful for fast smoke tests.")
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
		fs.StringVar(&factsPath, "facts", "", "optional path to extracted-facts JSONL (produced by locomo-extract) to inject alongside raw turns")
		fs.IntVar(&factsTopK, "facts-topk", 0, "if >0, do a dedicated fact-only retrieval pass for top-N facts and add them as a separate context block (in addition to top-K raw turns). Recommended: 5.")
		fs.StringVar(&profilesPath, "profiles", "", "optional path to entity-profiles JSONL (produced by locomo-profiles) to inject as dense entity_profile memories")
		fs.IntVar(&profilesTopK, "profiles-topk", 0, "if >0, do a dedicated entity-profile retrieval pass for top-N profiles and add them as a separate context block")
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

	var factsBySample map[string][]locomoFactRecord
	if factsPath != "" {
		factsBySample, err = loadLoCoMoFacts(factsPath)
		if err != nil {
			return fmt.Errorf("load facts: %w", err)
		}
		total := 0
		for _, fs := range factsBySample {
			total += len(fs)
		}
		fmt.Fprintf(os.Stderr, "loaded %d facts across %d records from %s\n", total, len(factsBySample), factsPath)
	}

	var profilesBySample map[string][]locomoProfileRecord
	if profilesPath != "" {
		profilesBySample, err = loadLoCoMoProfiles(profilesPath)
		if err != nil {
			return fmt.Errorf("load profiles: %w", err)
		}
		total := 0
		for _, ps := range profilesBySample {
			total += len(ps)
		}
		fmt.Fprintf(os.Stderr, "loaded %d entity profiles across %d records from %s\n", total, len(profilesBySample), profilesPath)
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
		// Inject extracted facts (if provided) as additional memories so the
		// FTS+embed retrieval can surface a one-line summary instead of
		// requiring the relevant raw turn to land in top-K. Stored as
		// SourceKind=extracted_fact and Role=fact for downstream auditing.
		if facts := factsBySample[rec.SampleID]; len(facts) > 0 {
			for _, f := range facts {
				ref := f.SessionID + ":" + f.Subject + ":" + f.Predicate
				if len(ref) > 80 {
					ref = ref[:80]
				}
				evJSON, _ := json.Marshal(f.EvidenceIDs)
				meta := fmt.Sprintf(`{"session_date":%q,"subject":%q,"predicate":%q,"object":%q,"evidence_dia_ids":%s}`,
					f.SessionDate, f.Subject, f.Predicate, f.Object, string(evJSON))
				_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
					Role:         "fact",
					Content:      f.FactText,
					SourceKind:   "extracted_fact",
					SourcePath:   "fact/" + f.SessionID,
					SourceRef:    ref,
					ScopeKind:    "project",
					ScopeID:      scopeID,
					Validity:     "active",
					MetadataJSON: meta,
				})
				if err != nil {
					st.Close()
					os.RemoveAll(tmpDir)
					return err
				}
			}
		}
		// Inject entity profiles (Track C). One dense memory per entity so a
		// single retrieval brings back the entity's full biographical context.
		if profs := profilesBySample[rec.SampleID]; len(profs) > 0 {
			for _, p := range profs {
				ref := p.Entity
				if len(ref) > 80 {
					ref = ref[:80]
				}
				meta := fmt.Sprintf(`{"entity":%q,"fact_count":%d}`, p.Entity, p.FactCount)
				_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
					Role:         "profile",
					Content:      p.Profile,
					SourceKind:   "entity_profile",
					SourcePath:   "entity/" + p.Entity,
					SourceRef:    ref,
					ScopeKind:    "project",
					ScopeID:      scopeID,
					Validity:     "active",
					MetadataJSON: meta,
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
			h := scoreLoCoMoQA(j.prep, j.qa, j.qIndex, mode, topK, model, client, maxTokens, reasoningEffort, hc, factsTopK, profilesTopK)
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
	enqueued := 0
outer:
	for _, p := range preps {
		for qi, qa := range p.rec.QA {
			if maxQuestions > 0 && enqueued >= maxQuestions {
				break outer
			}
			jobs <- job{idx: idx, prep: p, qa: qa, qIndex: qi}
			idx++
			enqueued++
		}
	}
	if maxQuestions > 0 && enqueued < len(results) {
		results = results[:enqueued]
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

func scoreLoCoMoQA(p *locomoPrepared, qa locomoQA, qIndex int, mode retrievalMode, topK int, model string, client *OpenRouterClient, maxTokens int, reasoningEffort string, hc *hybridConfig, factsTopK, profilesTopK int) locomoHypothesis {
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

	// Strip extracted_fact and entity_profile rows out of the raw-turns
	// leg so they don't crowd out actual turns; they re-enter via their
	// dedicated legs.
	if factsTopK > 0 || profilesTopK > 0 {
		var nonAux []store.Memory
		for _, r := range rows {
			if r.SourceKind == "extracted_fact" || r.SourceKind == "entity_profile" {
				continue
			}
			nonAux = append(nonAux, r)
		}
		rows = nonAux
	}

	var profileRows []store.Memory
	if profilesTopK > 0 {
		pPoolLimit := profilesTopK
		if hc != nil && hc.ftsPoolSize > pPoolLimit {
			pPoolLimit = hc.ftsPoolSize
		}
		if pPoolLimit > 100 {
			pPoolLimit = 100
		}
		pRows, perr := p.st.Search(ctx, store.SearchParams{
			Query:        qa.Question,
			ScopeKind:    "project",
			ScopeID:      p.scopeID,
			SourceKind:   "entity_profile",
			Limit:        pPoolLimit,
			Lifecycle:    store.LifecycleAny,
			SignalRerank: true,
		})
		if perr == nil && len(pRows) > 0 {
			if hc != nil && len(pRows) > 1 {
				ctxEmb, cancelEmb := context.WithTimeout(ctx, 60*time.Second)
				reranked, rerr := hybridRerankCandidates(ctxEmb, hc, qa.Question, pRows, profilesTopK)
				cancelEmb()
				if rerr == nil {
					pRows = reranked
				} else if len(pRows) > profilesTopK {
					pRows = pRows[:profilesTopK]
				}
			} else if len(pRows) > profilesTopK {
				pRows = pRows[:profilesTopK]
			}
			profileRows = pRows
		}
	}

	var factRows []store.Memory
	if factsTopK > 0 {
		fPoolLimit := factsTopK
		if hc != nil && hc.ftsPoolSize > fPoolLimit {
			fPoolLimit = hc.ftsPoolSize
		}
		if fPoolLimit > 100 {
			fPoolLimit = 100
		}
		fRows, ferr := p.st.Search(ctx, store.SearchParams{
			Query:        qa.Question,
			ScopeKind:    "project",
			ScopeID:      p.scopeID,
			SourceKind:   "extracted_fact",
			Limit:        fPoolLimit,
			Lifecycle:    store.LifecycleAny,
			SignalRerank: true,
		})
		if ferr == nil && len(fRows) > 0 {
			// Apply hybrid embedding rerank to the fact pool too, so semantic
			// matches (e.g. "Caroline is a single parent" for "what is
			// Caroline's relationship status?") surface despite zero keyword
			// overlap.
			if hc != nil && len(fRows) > 1 {
				ctxEmb, cancelEmb := context.WithTimeout(ctx, 60*time.Second)
				reranked, rerr := hybridRerankCandidates(ctxEmb, hc, qa.Question, fRows, factsTopK)
				cancelEmb()
				if rerr == nil {
					fRows = reranked
				} else if len(fRows) > factsTopK {
					fRows = fRows[:factsTopK]
				}
			} else if len(fRows) > factsTopK {
				fRows = fRows[:factsTopK]
			}
			factRows = fRows
		}
	}

	prompt := buildLoCoMoPrompt(qa, rows, factRows, profileRows)
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
// LongMemEval Chain-of-Note recipe but turn-grained. Session date is parsed
// out of MetadataJSON and inlined per turn so temporal-reasoning questions can
// resolve "yesterday" / "last year" against an absolute reference date.
func buildLoCoMoPrompt(qa locomoQA, rows []store.Memory, factRows []store.Memory, profileRows []store.Memory) string {
	var b strings.Builder
	b.WriteString("I will give you several turns from a long-running conversation between two people. ")
	b.WriteString("Each turn is tagged with the date the session occurred. ")
	if len(factRows) > 0 || len(profileRows) > 0 {
		b.WriteString("You may also see pre-extracted facts and/or entity profiles distilled from earlier turns — treat them as an index but cite the underlying turns when reasoning. ")
	}
	b.WriteString("Please answer the question based on the relevant turns. ")
	b.WriteString("Answer step by step: first extract relevant facts (resolving relative dates like \"yesterday\" or \"last year\" against the session date), then reason to the final answer.\n\n")
	if len(profileRows) > 0 {
		b.WriteString("Entity profiles:\n")
		for _, m := range profileRows {
			fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(m.Content))
		}
		b.WriteString("\n")
	}
	if len(factRows) > 0 {
		b.WriteString("Pre-extracted facts:\n")
		for i, m := range factRows {
			fmt.Fprintf(&b, "- [F%d] %s\n", i+1, strings.TrimSpace(m.Content))
		}
		b.WriteString("\n")
	}
	b.WriteString("Retrieved turns:\n")
	for i, m := range rows {
		sessionDate := extractSessionDate(m.MetadataJSON)
		fmt.Fprintf(&b, "\n### Turn %d (dia_id=%s, session=%s, session_date=%s):\n%s\n",
			i+1, m.SourceRef, strings.TrimPrefix(m.SourcePath, "session/"), sessionDate, m.Content)
	}
	if qa.Category == 5 {
		fmt.Fprintf(&b, "\nQuestion: %s\nIf the retrieved turns do not contain enough information to answer, respond \"I don't know\" or \"the information is not available.\"\nAnswer (step by step), then on the final line write \"Final answer: <concise answer>\":", qa.Question)
	} else {
		fmt.Fprintf(&b, "\nQuestion: %s\nAnswer (step by step), then on the final line write \"Final answer: <concise answer>\":", qa.Question)
	}
	return b.String()
}

// extractSessionDate pulls the session_date string out of the metadata JSON
// we wrote at ingest time. Returns "" if not present so the prompt degrades
// gracefully.
func extractSessionDate(metadataJSON string) string {
	var m struct {
		SessionDate string `json:"session_date"`
	}
	if metadataJSON == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(metadataJSON), &m); err != nil {
		return ""
	}
	return m.SessionDate
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
