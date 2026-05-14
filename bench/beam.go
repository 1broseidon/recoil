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
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

// BEAM (Beyond a Million Tokens — Tavakoli et al, ICLR 2026; arXiv 2510.27246)
// is the memory benchmark designed for production-scale agent contexts
// (100K → 500K → 1M → 10M tokens). Each conversation has 10 probing-question
// categories: abstention, contradiction_resolution, event_ordering,
// information_extraction, instruction_following, knowledge_update,
// multi_session_reasoning, preference_following, summarization,
// temporal_reasoning.
//
// Every question (except abstention) carries `source_chat_ids` — the integer
// turn IDs in chat.json that justify the answer. Our retrieval score is:
// did recoil's top-K include at least one of those source_chat_ids?
//
// The dataset is downloaded by bench/fetch_beam.sh:
//
//	bash bench/fetch_beam.sh 100K
//	bash bench/fetch_beam.sh 1M    # ~140 MB
//	bash bench/fetch_beam.sh 10M   # ~1.4 GB
//
// And lives at bench/.corpus/beam/<scale>/<conv>/{chat.json,probing_questions/...}.

const defaultBEAMScale = "100K"

// beamTurn is one utterance in chat.json. The id is what probing-question
// source_chat_ids references.
type beamTurn struct {
	ID           int    `json:"id"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	TimeAnchor   string `json:"time_anchor,omitempty"`
	Index        string `json:"index,omitempty"`
	QuestionType string `json:"question_type,omitempty"`
}

// beamBatch is one batch in chat.json (BEAM organizes conversations into
// 3+ batches, each containing a list of turn-groups).
type beamBatch struct {
	BatchNumber int          `json:"batch_number"`
	TimeAnchor  string       `json:"time_anchor"`
	Turns       [][]beamTurn `json:"turns"`
}

// beamQuestion is one probing question, generic shape across all categories.
// We only care about question text, source_chat_ids (flattened across the
// dict-vs-list variants), and a few optional reference fields used as fallback.
type beamQuestion struct {
	Question               string          `json:"question"`
	IdealResponse          string          `json:"ideal_response,omitempty"`
	IdealAnswer            string          `json:"ideal_answer,omitempty"`
	Answer                 any             `json:"answer,omitempty"`
	Difficulty             string          `json:"difficulty,omitempty"`
	SourceChatIDs          json.RawMessage `json:"source_chat_ids"`
	ConversationReferences json.RawMessage `json:"conversation_references,omitempty"`
	ConversationReference  string          `json:"conversation_reference,omitempty"`
	PlanReference          string          `json:"plan_reference,omitempty"`
}

type beamProbing map[string][]beamQuestion

type beamQuestionResult struct {
	ConversationID  string  `json:"conversation_id"`
	Category        string  `json:"category"`
	Question        string  `json:"question"`
	Recall5         float64 `json:"recall_at_5"`
	Recall10        float64 `json:"recall_at_10"`
	HitRank         int     `json:"hit_rank"`
	NumSourceIDs    int     `json:"num_source_ids"`
	RetrievedCount  int     `json:"retrieved_count"`
	SearchMillis    int64   `json:"search_ms"`
	Abstention      bool    `json:"abstention"`
}

type beamCategorySummary struct {
	Count    int     `json:"count"`
	Recall5  float64 `json:"recall_at_5"`
	Recall10 float64 `json:"recall_at_10"`
}

type beamSummary struct {
	Dataset             string                          `json:"dataset"`
	Scale               string                          `json:"scale"`
	StartedAt           string                          `json:"started_at"`
	FinishedAt          string                          `json:"finished_at"`
	Conversations       int                             `json:"conversations"`
	TotalQuestions      int                             `json:"total_questions"`
	ScoredQuestions     int                             `json:"scored_questions"`
	AbstentionCount     int                             `json:"abstention_count"`
	TopK                int                             `json:"top_k"`
	OverallRecall5      float64                         `json:"overall_recall_at_5"`
	OverallRecall10     float64                         `json:"overall_recall_at_10"`
	MeanReciprocalRank  float64                         `json:"mean_reciprocal_rank"`
	ByCategory          map[string]beamCategorySummary  `json:"by_category"`
	IngestSecondsTotal  float64                         `json:"ingest_seconds_total"`
	SearchSecondsTotal  float64                         `json:"search_seconds_total"`
	LatencySecondsP50   float64                         `json:"latency_seconds_p50"`
	LatencySecondsP95   float64                         `json:"latency_seconds_p95"`
	TotalTurnsIngested  int                             `json:"total_turns_ingested"`
}

func runBEAM(args []string) error {
	var dataDir string
	var scale string
	var topK int
	var limit int
	var verbose bool
	var resultsPath string
	fs, err := parseFlags("beam", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataDir, "data", "", "path to bench/.corpus/beam/<scale>/ (default: derive from --scale)")
		fs.StringVar(&scale, "scale", defaultBEAMScale, "BEAM scale: 100K | 500K | 1M | 10M")
		fs.IntVar(&topK, "top-k", 10, "retrieval depth")
		fs.IntVar(&limit, "limit", 0, "only run the first N conversations (0 = all)")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write per-question JSONL to this path")
	})
	if err != nil {
		return err
	}
	_ = fs

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

	convDirs, err := listConvDirs(dataDir)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(convDirs) {
		convDirs = convDirs[:limit]
	}
	fmt.Fprintf(os.Stderr, "BEAM scale=%s conversations=%d data=%s\n", scale, len(convDirs), dataDir)

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		resultsPath = filepath.Join(outDir, fmt.Sprintf("beam_%s_%s.jsonl", strings.ToLower(scale), runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()
	enc := json.NewEncoder(resultsFile)

	summary := beamSummary{
		Dataset:       "BEAM",
		Scale:         scale,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Conversations: len(convDirs),
		TopK:          topK,
		ByCategory:    map[string]beamCategorySummary{},
	}

	type catAgg struct {
		count   int
		hit5    int
		hit10   int
		rrSum   float64
		rrCount int
	}
	overall := &catAgg{}
	cats := map[string]*catAgg{}
	latencies := make([]float64, 0, 64)

	for ci, convDir := range convDirs {
		convID := filepath.Base(convDir)
		chatPath := filepath.Join(convDir, "chat.json")
		probingPath := filepath.Join(convDir, "probing_questions", "probing_questions.json")

		batches, err := loadBEAMChat(chatPath)
		if err != nil {
			return fmt.Errorf("conv %s: load chat.json: %w", convID, err)
		}
		probing, err := loadBEAMProbing(probingPath)
		if err != nil {
			return fmt.Errorf("conv %s: load probing: %w", convID, err)
		}

		totalTurns := 0
		for _, b := range batches {
			for _, tg := range b.Turns {
				totalTurns += len(tg)
			}
		}
		totalQ := 0
		for _, qs := range probing {
			totalQ += len(qs)
		}
		fmt.Fprintf(os.Stderr, "[conv %d/%d id=%s] turns=%d questions=%d\n",
			ci+1, len(convDirs), convID, totalTurns, totalQ)

		tmpDir, err := os.MkdirTemp("", "recoil-beam-")
		if err != nil {
			return err
		}
		dbPath := filepath.Join(tmpDir, "bench.db")
		st, err := store.Open(dbPath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return err
		}
		scopeID := "beam:" + scale + ":" + convID
		ctx := context.Background()

		t0 := time.Now()
		ingested := 0
		for _, batch := range batches {
			for _, turnGroup := range batch.Turns {
				for _, turn := range turnGroup {
					text := strings.TrimSpace(turn.Content)
					if text == "" {
						continue
					}
					content := turn.Role + ": " + text
					_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
						Role:       "source",
						Content:    content,
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
						st.Close()
						os.RemoveAll(tmpDir)
						return fmt.Errorf("add turn %d: %w", turn.ID, err)
					}
					ingested++
				}
			}
		}
		ingestSecs := time.Since(t0).Seconds()
		summary.IngestSecondsTotal += ingestSecs
		summary.TotalTurnsIngested += ingested

		catNames := make([]string, 0, len(probing))
		for k := range probing {
			catNames = append(catNames, k)
		}
		sort.Strings(catNames)
		for _, cat := range catNames {
			for _, qa := range probing[cat] {
				sourceIDs := flattenSourceChatIDs(qa.SourceChatIDs)
				if len(sourceIDs) == 0 {
					sourceIDs = append(sourceIDs, parseConvRefChatIDs(qa.ConversationReferences)...)
				}
				abstention := cat == "abstention"
				summary.TotalQuestions++

				t1 := time.Now()
				rows, err := st.Search(ctx, store.SearchParams{
					Query:        qa.Question,
					ScopeKind:    "project",
					ScopeID:      scopeID,
					Limit:        topK,
					Lifecycle:    store.LifecycleAny,
					SignalRerank: true,
				})
				searchMs := time.Since(t1).Milliseconds()
				if err != nil {
					st.Close()
					os.RemoveAll(tmpDir)
					return fmt.Errorf("search %s/%s: %w", convID, cat, err)
				}
				searchSecs := float64(searchMs) / 1000.0
				summary.SearchSecondsTotal += searchSecs
				latencies = append(latencies, searchSecs)

				sourceSet := make(map[int]bool, len(sourceIDs))
				for _, id := range sourceIDs {
					sourceSet[id] = true
				}
				hitRank := 0
				for i, row := range rows {
					id, err := strconv.Atoi(strings.TrimSpace(row.SourceRef))
					if err != nil {
						continue
					}
					if sourceSet[id] {
						hitRank = i + 1
						break
					}
				}
				result := beamQuestionResult{
					ConversationID: convID,
					Category:       cat,
					Question:       qa.Question,
					HitRank:        hitRank,
					NumSourceIDs:   len(sourceIDs),
					RetrievedCount: len(rows),
					SearchMillis:   searchMs,
					Abstention:     abstention,
				}
				if hitRank > 0 && hitRank <= 5 {
					result.Recall5 = 1.0
				}
				if hitRank > 0 && hitRank <= 10 {
					result.Recall10 = 1.0
				}
				if err := enc.Encode(result); err != nil {
					st.Close()
					os.RemoveAll(tmpDir)
					return err
				}

				if abstention {
					summary.AbstentionCount++
					continue
				}
				if len(sourceIDs) == 0 {
					// Non-abstention with no labeled source — skip scoring.
					continue
				}
				overall.count++
				if result.Recall5 > 0 {
					overall.hit5++
				}
				if result.Recall10 > 0 {
					overall.hit10++
				}
				if hitRank > 0 {
					overall.rrSum += 1.0 / float64(hitRank)
					overall.rrCount++
				}
				c, ok := cats[cat]
				if !ok {
					c = &catAgg{}
					cats[cat] = c
				}
				c.count++
				if result.Recall5 > 0 {
					c.hit5++
				}
				if result.Recall10 > 0 {
					c.hit10++
				}
				if verbose {
					fmt.Fprintf(os.Stderr, "  %s/%s r@5=%.0f rank=%d  %q\n",
						cat, convID, result.Recall5, hitRank, truncateQ(qa.Question, 60))
				}
			}
		}
		st.Close()
		os.RemoveAll(tmpDir)
		r5 := safeDiv(overall.hit5, overall.count)
		fmt.Fprintf(os.Stderr, "  [running] r@5=%.4f r@10=%.4f scored=%d\n",
			r5, safeDiv(overall.hit10, overall.count), overall.count)
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
		summary.ByCategory[k] = beamCategorySummary{
			Count:    c.count,
			Recall5:  safeDiv(c.hit5, c.count),
			Recall10: safeDiv(c.hit10, c.count),
		}
	}
	sort.Float64s(latencies)
	if n := len(latencies); n > 0 {
		summary.LatencySecondsP50 = latencies[n/2]
		idx95 := int(math.Min(float64(n-1), float64(n)*0.95))
		summary.LatencySecondsP95 = latencies[idx95]
	}
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	summaryPath := filepath.Join(outDir, fmt.Sprintf("beam_%s_%s_summary.json", strings.ToLower(scale), runID))
	if err := writeJSONFile(summaryPath, summary); err != nil {
		return err
	}
	mdPath := filepath.Join(outDir, fmt.Sprintf("beam_%s_%s.md", strings.ToLower(scale), runID))
	if err := writeBEAMMarkdown(mdPath, summary); err != nil {
		return err
	}
	printBEAMSummary(os.Stdout, summary)
	fmt.Fprintf(os.Stderr, "\nresults:  %s\nsummary:  %s\nmarkdown: %s\n", resultsPath, summaryPath, mdPath)
	return nil
}

func listConvDirs(scaleDir string) ([]string, error) {
	entries, err := os.ReadDir(scaleDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, filepath.Join(scaleDir, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool {
		ii, _ := strconv.Atoi(filepath.Base(out[i]))
		jj, _ := strconv.Atoi(filepath.Base(out[j]))
		return ii < jj
	})
	return out, nil
}

func loadBEAMChat(path string) ([]beamBatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var batches []beamBatch
	if err := json.Unmarshal(data, &batches); err != nil {
		return nil, err
	}
	return batches, nil
}

func loadBEAMProbing(path string) (beamProbing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var p beamProbing
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return p, nil
}

// flattenSourceChatIDs handles BEAM's two source_chat_ids shapes:
//   - list of ints: [10, 25]
//   - object with named buckets: {"first_statement":[58],"second_statement":[24]}
//
// Returns deduplicated int IDs.
func flattenSourceChatIDs(raw json.RawMessage) []int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// Try list first.
	var asList []any
	if err := json.Unmarshal(raw, &asList); err == nil {
		return collectInts(asList)
	}
	// Then dict.
	var asDict map[string]any
	if err := json.Unmarshal(raw, &asDict); err == nil {
		out := []int{}
		for _, v := range asDict {
			if l, ok := v.([]any); ok {
				out = append(out, collectInts(l)...)
			} else if n, ok := asInt(v); ok {
				out = append(out, n)
			}
		}
		return dedupInts(out)
	}
	return nil
}

func collectInts(xs []any) []int {
	out := make([]int, 0, len(xs))
	for _, x := range xs {
		if n, ok := asInt(x); ok {
			out = append(out, n)
		}
	}
	return dedupInts(out)
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n, true
		}
	}
	return 0, false
}

func dedupInts(xs []int) []int {
	seen := map[int]bool{}
	out := xs[:0]
	for _, x := range xs {
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

// parseConvRefChatIDs extracts integer chat_ids from BEAM's variant
// conversation_references shapes:
//   - list of objects: [{"chat_id": 58}, {"chat_id": 24}]
//   - list of strings: ["chat_id: 58", "chat_id: 24"]
//   - list of ints: [58, 24]
//   - single string or int
func parseConvRefChatIDs(raw json.RawMessage) []int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	out := []int{}
	// Try list of any.
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, x := range arr {
			switch v := x.(type) {
			case map[string]any:
				if cid, ok := v["chat_id"]; ok {
					if n, ok := asInt(cid); ok {
						out = append(out, n)
					}
				}
			case string:
				for _, tok := range strings.Fields(v) {
					tok = strings.TrimSuffix(tok, ",")
					tok = strings.TrimPrefix(tok, ":")
					if n, err := strconv.Atoi(tok); err == nil {
						out = append(out, n)
					}
				}
			case float64:
				out = append(out, int(v))
			}
		}
		return dedupInts(out)
	}
	// Single string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for _, tok := range strings.Fields(s) {
			tok = strings.TrimSuffix(tok, ",")
			if n, err := strconv.Atoi(tok); err == nil {
				out = append(out, n)
			}
		}
		return dedupInts(out)
	}
	return nil
}

func firstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func writeBEAMMarkdown(path string, s beamSummary) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# BEAM run — scale %s\n\n", s.Scale)
	fmt.Fprintf(b, "Conversations: %d  ·  questions scored: %d (abstention skipped: %d)  ·  top-K: %d\n\n",
		s.Conversations, s.ScoredQuestions, s.AbstentionCount, s.TopK)
	fmt.Fprintf(b, "**Turn-grain retrieval (top-K contains a source_chat_id):**\n\n")
	fmt.Fprintf(b, "- R@5  = %.4f\n- R@10 = %.4f\n- MRR  = %.4f\n\n",
		s.OverallRecall5, s.OverallRecall10, s.MeanReciprocalRank)
	fmt.Fprintf(b, "**Per category:**\n\n")
	fmt.Fprintln(b, "| category | n | R@5 | R@10 |")
	fmt.Fprintln(b, "|---|---|---|---|")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(b, "| %s | %d | %.4f | %.4f |\n", k, c.Count, c.Recall5, c.Recall10)
	}
	fmt.Fprintf(b, "\nIngested %d turns total across %d conversations in %.1fs.\n",
		s.TotalTurnsIngested, s.Conversations, s.IngestSecondsTotal)
	fmt.Fprintf(b, "Search latency: p50 = %.3fs, p95 = %.3fs.\n",
		s.LatencySecondsP50, s.LatencySecondsP95)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func printBEAMSummary(w io.Writer, s beamSummary) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "scale:                  %s\n", s.Scale)
	fmt.Fprintf(w, "conversations:          %d\n", s.Conversations)
	fmt.Fprintf(w, "scored questions:       %d\n", s.ScoredQuestions)
	fmt.Fprintf(w, "abstention skipped:     %d\n", s.AbstentionCount)
	fmt.Fprintf(w, "top_k:                  %d\n", s.TopK)
	fmt.Fprintf(w, "turn  recall@5:         %.4f\n", s.OverallRecall5)
	fmt.Fprintf(w, "turn  recall@10:        %.4f\n", s.OverallRecall10)
	fmt.Fprintf(w, "mean reciprocal rank:   %.4f\n", s.MeanReciprocalRank)
	fmt.Fprintf(w, "latency p50:            %.3fs\n", s.LatencySecondsP50)
	fmt.Fprintf(w, "latency p95:            %.3fs\n", s.LatencySecondsP95)
	fmt.Fprintf(w, "total turns ingested:   %d\n", s.TotalTurnsIngested)
	fmt.Fprintf(w, "total ingest seconds:   %.2f\n", s.IngestSecondsTotal)
	fmt.Fprintln(w, "---")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(w, "%-26s n=%4d r@5=%.4f r@10=%.4f\n", k, c.Count, c.Recall5, c.Recall10)
	}
}
