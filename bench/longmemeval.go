package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

const sessionPathPrefix = "session/"

type lmeTurn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer bool   `json:"has_answer,omitempty"`
}

type lmeQuestion struct {
	QuestionID         string      `json:"question_id"`
	QuestionType       string      `json:"question_type"`
	Question           string      `json:"question"`
	Answer             any         `json:"answer"`
	QuestionDate       string      `json:"question_date"`
	HaystackSessionIDs []string    `json:"haystack_session_ids"`
	HaystackDates      []string    `json:"haystack_dates"`
	HaystackSessions   [][]lmeTurn `json:"haystack_sessions"`
	AnswerSessionIDs   []string    `json:"answer_session_ids"`
}

type questionResult struct {
	QuestionID     string  `json:"question_id"`
	QuestionType   string  `json:"question_type"`
	Recall5        float64 `json:"recall_at_5"`
	Recall10       float64 `json:"recall_at_10"`
	HitRank        int     `json:"hit_rank"` // 1-indexed rank of first labelled answer session, 0 if none
	IngestMillis   int64   `json:"ingest_ms"`
	SearchMillis   int64   `json:"search_ms"`
	NumSessions    int     `json:"num_sessions"`
	NumAnswerSIDs  int     `json:"num_answer_sids"`
	RetrievedCount int     `json:"retrieved_count"`
	Abstention     bool    `json:"abstention"`
}

type benchmarkSummary struct {
	Dataset            string                     `json:"dataset"`
	StartedAt          string                     `json:"started_at"`
	FinishedAt         string                     `json:"finished_at"`
	TotalQuestions     int                        `json:"total_questions"`
	ScoredQuestions    int                        `json:"scored_questions"` // excludes abstention
	AbstentionCount    int                        `json:"abstention_count"`
	TopK               int                        `json:"top_k"`
	OverallRecall5     float64                    `json:"overall_recall_at_5"`
	OverallRecall10    float64                    `json:"overall_recall_at_10"`
	MeanReciprocalRank float64                    `json:"mean_reciprocal_rank"`
	ByQuestionType     map[string]categorySummary `json:"by_question_type"`
	LatencySecondsP50  float64                    `json:"latency_seconds_p50"`
	LatencySecondsP95  float64                    `json:"latency_seconds_p95"`
	IngestSecondsTotal float64                    `json:"ingest_seconds_total"`
	SearchSecondsTotal float64                    `json:"search_seconds_total"`
}

type categorySummary struct {
	Count    int     `json:"count"`
	Recall5  float64 `json:"recall_at_5"`
	Recall10 float64 `json:"recall_at_10"`
}

func runLongMemEval(args []string) error {
	var dataPath string
	var topK int
	var limit int
	var verbose bool
	var resultsPath string
	var hybridEmbedding bool
	var embedProvider string
	var embedModel string
	var ollamaHost string
	var maxEmbedChars int
	var chunkChars int
	var chunkOverlap int
	var fusionK int
	var ftsPool int
	var embedPool int
	fs, err := parseFlags("longmemeval", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to longmemeval_s_cleaned.json (default: bench/.corpus/)")
		fs.IntVar(&topK, "top-k", 10, "max retrieval depth (final fused result size when --hybrid-embedding)")
		fs.IntVar(&limit, "limit", 0, "only run the first N questions (0 = all)")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write results JSONL to this path (default: bench/results/...)")
		fs.BoolVar(&hybridEmbedding, "hybrid-embedding", false, "fuse FTS5 with cosine-similarity over embeddings")
		fs.StringVar(&embedProvider, "embed-provider", "openrouter", "embedding provider: openrouter | ollama")
		fs.StringVar(&embedModel, "embed-model", "", "embedding model (defaults: openrouter -> openai/text-embedding-3-small, ollama -> nomic-embed-text)")
		fs.StringVar(&ollamaHost, "ollama-host", "", "Ollama base URL (default: http://localhost:11434, overridable via OLLAMA_HOST)")
		fs.IntVar(&maxEmbedChars, "max-embed-chars", 0, "truncate each embedding input to this many chars (default: 6000 for ollama, unlimited for openrouter)")
		fs.IntVar(&chunkChars, "chunk-chars", 0, "chunk each session into windows of this many chars (max-pool cosine over chunks). 0 = truncate via --max-embed-chars instead")
		fs.IntVar(&chunkOverlap, "chunk-overlap", 500, "overlap between adjacent chunks in chars (when --chunk-chars > 0)")
		fs.IntVar(&fusionK, "fusion-k", 60, "RRF fusion constant (standard: 60)")
		fs.IntVar(&ftsPool, "fts-pool", 50, "FTS candidate pool for fusion")
		fs.IntVar(&embedPool, "embed-pool", 50, "embedding candidate pool for fusion")
	})
	if err != nil {
		return err
	}
	_ = fs

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
	fmt.Fprintf(os.Stderr, "loaded %d questions\n", len(questions))

	var hc *hybridConfig
	if hybridEmbedding {
		resolvedModel := embedModel
		hc = &hybridConfig{
			embedProvider: embedProvider,
			ollamaHost:    ollamaHost,
			maxEmbedChars: maxEmbedChars,
			chunkChars:    chunkChars,
			chunkOverlap:  chunkOverlap,
			fusionK:       fusionK,
			ftsPoolSize:   ftsPool,
			embedPoolSize: embedPool,
		}
		switch embedProvider {
		case "openrouter", "":
			if resolvedModel == "" {
				resolvedModel = "openai/text-embedding-3-small"
			}
			client, err := NewOpenRouterClient()
			if err != nil {
				return fmt.Errorf("embed-provider=openrouter requires OPENROUTER_API_KEY: %w", err)
			}
			hc.client = client
			hc.embedProvider = "openrouter"
		case "ollama":
			if resolvedModel == "" {
				resolvedModel = "nomic-embed-text"
			}
			if hc.maxEmbedChars == 0 {
				// nomic-embed-text caps at 2048 tokens (~8000 chars). 6000 leaves
				// headroom for tokenization overhead and rarely costs much signal
				// since key turns tend to be near the start of a session.
				hc.maxEmbedChars = 6000
			}
		default:
			return fmt.Errorf("unknown --embed-provider %q (supported: openrouter, ollama)", embedProvider)
		}
		hc.embedModel = resolvedModel
		fmt.Fprintf(os.Stderr, "hybrid retrieval: provider=%s model=%s rrf-k=%d fts-pool=%d embed-pool=%d max-embed-chars=%d\n",
			hc.embedProvider, hc.embedModel, fusionK, ftsPool, embedPool, hc.maxEmbedChars)
	}

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		suffix := ""
		if hybridEmbedding {
			suffix = "_hybrid"
		}
		resultsPath = filepath.Join(outDir, fmt.Sprintf("longmemeval%s_%s.jsonl", suffix, runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()
	enc := json.NewEncoder(resultsFile)

	summary := benchmarkSummary{
		Dataset:        filepath.Base(resolved),
		StartedAt:      time.Now().UTC().Format(time.RFC3339),
		TotalQuestions: len(questions),
		TopK:           topK,
		ByQuestionType: map[string]categorySummary{},
	}

	type catAggregator struct {
		count    int
		hit5     int
		hit10    int
		rrSum    float64
		rrCount  int
	}
	cats := map[string]*catAggregator{}
	overall := &catAggregator{}
	latencies := make([]float64, 0, len(questions))

	for i, q := range questions {
		var (
			result questionResult
			err    error
		)
		if hc != nil {
			result, err = scoreQuestionHybrid(q, topK, hc, verbose)
		} else {
			result, err = scoreQuestion(q, topK, verbose)
		}
		if err != nil {
			return fmt.Errorf("question %s: %w", q.QuestionID, err)
		}
		if err := enc.Encode(result); err != nil {
			return err
		}
		summary.IngestSecondsTotal += float64(result.IngestMillis) / 1000.0
		summary.SearchSecondsTotal += float64(result.SearchMillis) / 1000.0
		totalSec := float64(result.IngestMillis+result.SearchMillis) / 1000.0
		latencies = append(latencies, totalSec)

		if result.Abstention {
			summary.AbstentionCount++
		} else {
			overall.count++
			if result.Recall5 > 0 {
				overall.hit5++
			}
			if result.Recall10 > 0 {
				overall.hit10++
			}
			if result.HitRank > 0 {
				overall.rrSum += 1.0 / float64(result.HitRank)
				overall.rrCount++
			}
			c, ok := cats[q.QuestionType]
			if !ok {
				c = &catAggregator{}
				cats[q.QuestionType] = c
			}
			c.count++
			if result.Recall5 > 0 {
				c.hit5++
			}
			if result.Recall10 > 0 {
				c.hit10++
			}
			if result.HitRank > 0 {
				c.rrSum += 1.0 / float64(result.HitRank)
				c.rrCount++
			}
		}

		if (i+1)%25 == 0 || i+1 == len(questions) {
			r5 := 0.0
			if overall.count > 0 {
				r5 = float64(overall.hit5) / float64(overall.count)
			}
			fmt.Fprintf(os.Stderr, "[%4d/%4d] running recall@5 = %.4f  (last=%s recall@5=%.0f)\n",
				i+1, len(questions), r5, q.QuestionType, result.Recall5)
		}
	}

	if overall.count > 0 {
		summary.ScoredQuestions = overall.count
		summary.OverallRecall5 = float64(overall.hit5) / float64(overall.count)
		summary.OverallRecall10 = float64(overall.hit10) / float64(overall.count)
		if overall.rrCount > 0 {
			summary.MeanReciprocalRank = overall.rrSum / float64(overall.rrCount)
		}
	}
	for k, c := range cats {
		summary.ByQuestionType[k] = categorySummary{
			Count:    c.count,
			Recall5:  safeDiv(c.hit5, c.count),
			Recall10: safeDiv(c.hit10, c.count),
		}
	}
	sort.Float64s(latencies)
	if n := len(latencies); n > 0 {
		summary.LatencySecondsP50 = latencies[n/2]
		idx95 := int(float64(n) * 0.95)
		if idx95 >= n {
			idx95 = n - 1
		}
		summary.LatencySecondsP95 = latencies[idx95]
	}
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	summaryPath := filepath.Join(outDir, fmt.Sprintf("longmemeval_%s_summary.json", runID))
	if err := writeJSONFile(summaryPath, summary); err != nil {
		return err
	}
	resultsMDPath := filepath.Join(outDir, fmt.Sprintf("longmemeval_%s.md", runID))
	if err := writeResultsMarkdown(resultsMDPath, summary); err != nil {
		return err
	}

	printSummary(os.Stdout, summary)
	fmt.Fprintf(os.Stderr, "\nresults:  %s\nsummary:  %s\nmarkdown: %s\n", resultsPath, summaryPath, resultsMDPath)
	return nil
}

func loadLongMemEval(path string) ([]lmeQuestion, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var questions []lmeQuestion
	if err := json.Unmarshal(data, &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

func scoreQuestion(q lmeQuestion, topK int, verbose bool) (questionResult, error) {
	result := questionResult{
		QuestionID:    q.QuestionID,
		QuestionType:  q.QuestionType,
		NumSessions:   len(q.HaystackSessionIDs),
		NumAnswerSIDs: len(q.AnswerSessionIDs),
		Abstention:    strings.HasSuffix(q.QuestionID, "_abs"),
	}

	tmpDir, err := os.MkdirTemp("", "recoil-bench-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "bench.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return result, err
	}
	defer st.Close()

	scopeID := "lme:" + q.QuestionID
	ctx := context.Background()

	t0 := time.Now()
	for i, sid := range q.HaystackSessionIDs {
		if i >= len(q.HaystackSessions) {
			break
		}
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
			return result, fmt.Errorf("adding session %s: %w", sid, err)
		}
	}
	result.IngestMillis = time.Since(t0).Milliseconds()

	t1 := time.Now()
	rows, err := st.Search(ctx, store.SearchParams{
		Query:     q.Question,
		ScopeKind: "project",
		ScopeID:   scopeID,
		Limit:     topK,
		Lifecycle: store.LifecycleAny,
	})
	if err != nil {
		return result, fmt.Errorf("search: %w", err)
	}
	result.SearchMillis = time.Since(t1).Milliseconds()
	result.RetrievedCount = len(rows)

	if result.Abstention {
		return result, nil
	}

	answerSet := map[string]bool{}
	for _, sid := range q.AnswerSessionIDs {
		answerSet[sid] = true
	}
	hitRank := 0
	for i, mem := range rows {
		sid := strings.TrimPrefix(mem.SourcePath, sessionPathPrefix)
		if answerSet[sid] {
			if hitRank == 0 {
				hitRank = i + 1
			}
		}
	}
	result.HitRank = hitRank
	if hitRank > 0 && hitRank <= 5 {
		result.Recall5 = 1.0
	}
	if hitRank > 0 && hitRank <= 10 {
		result.Recall10 = 1.0
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "  %s (%s): r@5=%.0f r@10=%.0f hit_rank=%d sessions=%d\n",
			q.QuestionID, q.QuestionType, result.Recall5, result.Recall10, hitRank, result.NumSessions)
	}
	return result, nil
}

func formatSession(turns []lmeTurn) string {
	var b strings.Builder
	for _, t := range turns {
		role := strings.TrimSpace(t.Role)
		content := strings.TrimSpace(t.Content)
		if role == "" || content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
	}
	return b.String()
}

func safeDiv(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom)
}

func writeJSONFile(path string, value any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeResultsMarkdown(path string, s benchmarkSummary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "# LongMemEval — recoil baseline\n\n")
	fmt.Fprintf(f, "- Dataset: `%s`\n", s.Dataset)
	fmt.Fprintf(f, "- Started: %s\n", s.StartedAt)
	fmt.Fprintf(f, "- Finished: %s\n", s.FinishedAt)
	fmt.Fprintf(f, "- Top-K: %d\n", s.TopK)
	fmt.Fprintf(f, "- Questions: %d total, %d scored, %d abstention\n",
		s.TotalQuestions, s.ScoredQuestions, s.AbstentionCount)
	fmt.Fprintf(f, "\n## Overall\n\n")
	fmt.Fprintf(f, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(f, "| Recall@5 | **%.4f** |\n", s.OverallRecall5)
	fmt.Fprintf(f, "| Recall@10 | **%.4f** |\n", s.OverallRecall10)
	fmt.Fprintf(f, "| MRR | %.4f |\n", s.MeanReciprocalRank)
	fmt.Fprintf(f, "| Latency p50 (s) | %.3f |\n", s.LatencySecondsP50)
	fmt.Fprintf(f, "| Latency p95 (s) | %.3f |\n", s.LatencySecondsP95)
	fmt.Fprintf(f, "| Ingest total (s) | %.1f |\n", s.IngestSecondsTotal)
	fmt.Fprintf(f, "| Search total (s) | %.1f |\n", s.SearchSecondsTotal)
	fmt.Fprintf(f, "\n## By question type\n\n")
	fmt.Fprintf(f, "| Question type | Count | Recall@5 | Recall@10 |\n|---|---:|---:|---:|\n")
	keys := make([]string, 0, len(s.ByQuestionType))
	for k := range s.ByQuestionType {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByQuestionType[k]
		fmt.Fprintf(f, "| %s | %d | %.4f | %.4f |\n", k, c.Count, c.Recall5, c.Recall10)
	}
	fmt.Fprintf(f, "\n## Reference points (from MemPal's BENCHMARKS.md)\n\n")
	fmt.Fprintf(f, "| System | R@5 |\n|---|---:|\n")
	fmt.Fprintf(f, "| MemPal raw ChromaDB | 0.966 |\n")
	fmt.Fprintf(f, "| Stella (dense) | ~0.85 |\n")
	fmt.Fprintf(f, "| Contriever | ~0.78 |\n")
	fmt.Fprintf(f, "| BM25 | ~0.70 |\n")
	return nil
}

func printSummary(w io.Writer, s benchmarkSummary) {
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "dataset: %s\n", s.Dataset)
	fmt.Fprintf(w, "scored: %d\n", s.ScoredQuestions)
	fmt.Fprintf(w, "abstention: %d\n", s.AbstentionCount)
	fmt.Fprintf(w, "top_k: %d\n", s.TopK)
	fmt.Fprintf(w, "overall_recall_at_5: %.4f\n", s.OverallRecall5)
	fmt.Fprintf(w, "overall_recall_at_10: %.4f\n", s.OverallRecall10)
	fmt.Fprintf(w, "mean_reciprocal_rank: %.4f\n", s.MeanReciprocalRank)
	fmt.Fprintf(w, "latency_p50_s: %.3f\n", s.LatencySecondsP50)
	fmt.Fprintf(w, "latency_p95_s: %.3f\n", s.LatencySecondsP95)
	fmt.Fprintf(w, "---\n")
	keys := make([]string, 0, len(s.ByQuestionType))
	for k := range s.ByQuestionType {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByQuestionType[k]
		fmt.Fprintf(w, "%-32s  n=%4d  r@5=%.4f  r@10=%.4f\n", k, c.Count, c.Recall5, c.Recall10)
	}
}
