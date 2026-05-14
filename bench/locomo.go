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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

// LoCoMo (Maharana et al, ACL 2024 — arXiv 2402.17753) evaluates very-long-term
// conversational memory. Each record is a 19-session conversation between two
// speakers with ~419 turns and ~9k tokens, and ~199 QA pairs. Evidence is at
// the TURN level via dia_ids like "D1:3" (session 1, turn 3).
//
// We bench recoil's retrieval here: ingest every turn as its own memory tagged
// with its dia_id, then for each non-adversarial question run a search and
// check whether any retrieved turn's dia_id matches the evidence dia_ids.
// Adversarial questions (category 5) are treated as abstention — they have an
// "adversarial_answer" rather than an "answer" and the expected behavior is
// "do not surface a confident hit." We measure them separately.
//
// Categories (per the LoCoMo paper):
//   1 = single-hop factual recall
//   2 = temporal reasoning
//   3 = multi-hop reasoning
//   4 = open-domain knowledge
//   5 = adversarial (no grounded answer; the model should abstain)

const defaultLoCoMoFile = "locomo10.json"

// locomoTurn is one utterance in a session.
type locomoTurn struct {
	Speaker string `json:"speaker"`
	DiaID   string `json:"dia_id"`
	Text    string `json:"text"`
	// Some records use blip_caption / img_url for multimodal turns; we ignore those
	// and lean on Text only.
}

// locomoQA is one question with its evidence dia_ids and category label.
type locomoQA struct {
	Question          string   `json:"question"`
	Answer            any      `json:"answer,omitempty"`
	AdversarialAnswer string   `json:"adversarial_answer,omitempty"`
	Evidence          []string `json:"evidence"`
	Category          int      `json:"category"`
}

// locomoRecord is one conversation + its QA set.
type locomoRecord struct {
	SampleID     string          `json:"sample_id"`
	QA           []locomoQA      `json:"qa"`
	Conversation json.RawMessage `json:"conversation"`
}

type locomoSession struct {
	ID    string // "1", "2", ...
	Date  string
	Turns []locomoTurn
}

type locomoQuestionResult struct {
	SampleID     string `json:"sample_id"`
	Category     int    `json:"category"`
	CategoryName string `json:"category_name"`
	Question     string `json:"question"`
	Recall5      float64 `json:"recall_at_5"`
	Recall10     float64 `json:"recall_at_10"`
	HitRank      int    `json:"hit_rank"`
	NumEvidence  int    `json:"num_evidence"`
	RetrievedCt  int    `json:"retrieved_count"`
	SearchMillis int64  `json:"search_ms"`
	Abstention   bool   `json:"abstention"`
	// Session-grain recall: did the top-K include the right session(s)?
	SessionRecall5  float64 `json:"session_recall_at_5"`
	SessionRecall10 float64 `json:"session_recall_at_10"`
}

type locomoCategorySummary struct {
	Count           int     `json:"count"`
	Recall5         float64 `json:"recall_at_5"`
	Recall10        float64 `json:"recall_at_10"`
	SessionRecall5  float64 `json:"session_recall_at_5"`
	SessionRecall10 float64 `json:"session_recall_at_10"`
}

type locomoSummary struct {
	Dataset             string                          `json:"dataset"`
	StartedAt           string                          `json:"started_at"`
	FinishedAt          string                          `json:"finished_at"`
	Records             int                             `json:"records"`
	TotalQuestions      int                             `json:"total_questions"`
	ScoredQuestions     int                             `json:"scored_questions"`
	AbstentionCount     int                             `json:"abstention_count"`
	TopK                int                             `json:"top_k"`
	OverallRecall5      float64                         `json:"overall_recall_at_5"`
	OverallRecall10     float64                         `json:"overall_recall_at_10"`
	SessionRecall5      float64                         `json:"overall_session_recall_at_5"`
	SessionRecall10     float64                         `json:"overall_session_recall_at_10"`
	MeanReciprocalRank  float64                         `json:"mean_reciprocal_rank"`
	ByCategory          map[string]locomoCategorySummary `json:"by_category"`
	IngestSecondsTotal  float64                         `json:"ingest_seconds_total"`
	SearchSecondsTotal  float64                         `json:"search_seconds_total"`
	LatencySecondsP50   float64                         `json:"latency_seconds_p50"`
	LatencySecondsP95   float64                         `json:"latency_seconds_p95"`
}

func locomoCategoryName(cat int) string {
	switch cat {
	case 1:
		return "single-hop"
	case 2:
		return "temporal-reasoning"
	case 3:
		return "multi-hop"
	case 4:
		return "open-domain"
	case 5:
		return "adversarial"
	default:
		return fmt.Sprintf("cat-%d", cat)
	}
}

func runLoCoMo(args []string) error {
	var dataPath string
	var topK int
	var limit int
	var verbose bool
	var resultsPath string
	fs, err := parseFlags("locomo", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to locomo10.json (default: bench/.corpus/)")
		fs.IntVar(&topK, "top-k", 10, "retrieval depth")
		fs.IntVar(&limit, "limit", 0, "only run the first N records (0 = all 10)")
		fs.BoolVar(&verbose, "verbose", false, "print per-question results")
		fs.StringVar(&resultsPath, "out", "", "write per-question JSONL to this path")
	})
	if err != nil {
		return err
	}
	_ = fs

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
	totalQ := 0
	for _, r := range records {
		totalQ += len(r.QA)
	}
	fmt.Fprintf(os.Stderr, "loaded %d records, %d questions total\n", len(records), totalQ)

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	if resultsPath == "" {
		resultsPath = filepath.Join(outDir, fmt.Sprintf("locomo_%s.jsonl", runID))
	}
	resultsFile, err := os.Create(resultsPath)
	if err != nil {
		return err
	}
	defer resultsFile.Close()
	enc := json.NewEncoder(resultsFile)

	summary := locomoSummary{
		Dataset:        filepath.Base(resolved),
		StartedAt:      time.Now().UTC().Format(time.RFC3339),
		Records:        len(records),
		TotalQuestions: totalQ,
		TopK:           topK,
		ByCategory:     map[string]locomoCategorySummary{},
	}

	type catAgg struct {
		count          int
		hit5           int
		hit10          int
		sessHit5       int
		sessHit10      int
		rrSum          float64
		rrCount        int
	}
	overall := &catAgg{}
	cats := map[string]*catAgg{}
	latencies := make([]float64, 0, totalQ)

	for ri, rec := range records {
		sessions, err := parseConversation(rec.Conversation)
		if err != nil {
			return fmt.Errorf("record %d (%s): parse conversation: %w", ri, rec.SampleID, err)
		}
		fmt.Fprintf(os.Stderr, "[record %d/%d sample_id=%s] %d sessions, %d QA\n",
			ri+1, len(records), rec.SampleID, len(sessions), len(rec.QA))

		tmpDir, err := os.MkdirTemp("", "recoil-locomo-")
		if err != nil {
			return err
		}
		dbPath := filepath.Join(tmpDir, "bench.db")
		st, err := store.Open(dbPath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return err
		}
		scopeID := "locomo:" + rec.SampleID
		ctx := context.Background()

		t0 := time.Now()
		for _, sess := range sessions {
			for _, turn := range sess.Turns {
				text := strings.TrimSpace(turn.Text)
				if text == "" {
					continue
				}
				content := turn.Speaker + ": " + text
				_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
					Role:       "source",
					Content:    content,
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
					return fmt.Errorf("add turn %s: %w", turn.DiaID, err)
				}
			}
		}
		ingestSecs := time.Since(t0).Seconds()
		summary.IngestSecondsTotal += ingestSecs

		for _, qa := range rec.QA {
			abstention := qa.Category == 5
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
				return fmt.Errorf("search %s/%d: %w", rec.SampleID, qa.Category, err)
			}
			searchSecs := float64(searchMs) / 1000.0
			summary.SearchSecondsTotal += searchSecs
			latencies = append(latencies, searchSecs)

			evidenceSet := map[string]bool{}
			evidenceSessions := map[string]bool{}
			for _, e := range qa.Evidence {
				evidenceSet[strings.ToLower(strings.TrimSpace(e))] = true
				if sid, ok := sessionIDFromDiaID(e); ok {
					evidenceSessions[sid] = true
				}
			}

			hitRank := 0
			sessHitRank := 0
			for i, row := range rows {
				ref := strings.ToLower(strings.TrimSpace(row.SourceRef))
				if evidenceSet[ref] && hitRank == 0 {
					hitRank = i + 1
				}
				sid := strings.TrimPrefix(row.SourcePath, "session/")
				if evidenceSessions[sid] && sessHitRank == 0 {
					sessHitRank = i + 1
				}
			}

			result := locomoQuestionResult{
				SampleID:     rec.SampleID,
				Category:     qa.Category,
				CategoryName: locomoCategoryName(qa.Category),
				Question:     qa.Question,
				HitRank:      hitRank,
				NumEvidence:  len(qa.Evidence),
				RetrievedCt:  len(rows),
				SearchMillis: searchMs,
				Abstention:   abstention,
			}
			if hitRank > 0 && hitRank <= 5 {
				result.Recall5 = 1.0
			}
			if hitRank > 0 && hitRank <= 10 {
				result.Recall10 = 1.0
			}
			if sessHitRank > 0 && sessHitRank <= 5 {
				result.SessionRecall5 = 1.0
			}
			if sessHitRank > 0 && sessHitRank <= 10 {
				result.SessionRecall10 = 1.0
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
			overall.count++
			if result.Recall5 > 0 {
				overall.hit5++
			}
			if result.Recall10 > 0 {
				overall.hit10++
			}
			if result.SessionRecall5 > 0 {
				overall.sessHit5++
			}
			if result.SessionRecall10 > 0 {
				overall.sessHit10++
			}
			if hitRank > 0 {
				overall.rrSum += 1.0 / float64(hitRank)
				overall.rrCount++
			}
			catName := result.CategoryName
			c, ok := cats[catName]
			if !ok {
				c = &catAgg{}
				cats[catName] = c
			}
			c.count++
			if result.Recall5 > 0 {
				c.hit5++
			}
			if result.Recall10 > 0 {
				c.hit10++
			}
			if result.SessionRecall5 > 0 {
				c.sessHit5++
			}
			if result.SessionRecall10 > 0 {
				c.sessHit10++
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  cat=%d r@5=%.0f r@10=%.0f rank=%d  %q\n",
					qa.Category, result.Recall5, result.Recall10, hitRank, truncateQ(qa.Question, 60))
			}
		}
		st.Close()
		os.RemoveAll(tmpDir)
		// Running tally per record.
		r5 := safeDiv(overall.hit5, overall.count)
		fmt.Fprintf(os.Stderr, "  [running] turn-r@5=%.4f  sess-r@5=%.4f  (scored=%d)\n",
			r5, safeDiv(overall.sessHit5, overall.count), overall.count)
	}

	if overall.count > 0 {
		summary.ScoredQuestions = overall.count
		summary.OverallRecall5 = float64(overall.hit5) / float64(overall.count)
		summary.OverallRecall10 = float64(overall.hit10) / float64(overall.count)
		summary.SessionRecall5 = float64(overall.sessHit5) / float64(overall.count)
		summary.SessionRecall10 = float64(overall.sessHit10) / float64(overall.count)
		if overall.rrCount > 0 {
			summary.MeanReciprocalRank = overall.rrSum / float64(overall.rrCount)
		}
	}
	for k, c := range cats {
		summary.ByCategory[k] = locomoCategorySummary{
			Count:           c.count,
			Recall5:         safeDiv(c.hit5, c.count),
			Recall10:        safeDiv(c.hit10, c.count),
			SessionRecall5:  safeDiv(c.sessHit5, c.count),
			SessionRecall10: safeDiv(c.sessHit10, c.count),
		}
	}
	sort.Float64s(latencies)
	if n := len(latencies); n > 0 {
		summary.LatencySecondsP50 = latencies[n/2]
		idx95 := int(math.Min(float64(n-1), float64(n)*0.95))
		summary.LatencySecondsP95 = latencies[idx95]
	}
	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	summaryPath := filepath.Join(outDir, fmt.Sprintf("locomo_%s_summary.json", runID))
	if err := writeJSONFile(summaryPath, summary); err != nil {
		return err
	}
	mdPath := filepath.Join(outDir, fmt.Sprintf("locomo_%s.md", runID))
	if err := writeLoCoMoMarkdown(mdPath, summary); err != nil {
		return err
	}
	printLoCoMoSummary(os.Stdout, summary)
	fmt.Fprintf(os.Stderr, "\nresults:  %s\nsummary:  %s\nmarkdown: %s\n", resultsPath, summaryPath, mdPath)
	return nil
}

func loadLoCoMo(path string) ([]locomoRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var records []locomoRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	// Some records omit sample_id in the snap-research release. Generate a stable
	// fallback so downstream tooling has something to key off.
	for i := range records {
		if strings.TrimSpace(records[i].SampleID) == "" {
			records[i].SampleID = fmt.Sprintf("locomo-%02d", i+1)
		}
	}
	return records, nil
}

// parseConversation extracts ordered sessions out of the mixed-schema
// conversation object. Keys come in pairs: session_<N> (list of turns) and
// session_<N>_date_time (string). speaker_a / speaker_b are top-level strings
// we don't need here because each turn carries its own speaker.
func parseConversation(raw json.RawMessage) ([]locomoSession, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	type sessionAccum struct {
		date  string
		turns []locomoTurn
	}
	accum := map[string]*sessionAccum{}
	for key, val := range m {
		switch {
		case strings.HasSuffix(key, "_date_time") && strings.HasPrefix(key, "session_"):
			id := strings.TrimSuffix(strings.TrimPrefix(key, "session_"), "_date_time")
			s := accum[id]
			if s == nil {
				s = &sessionAccum{}
				accum[id] = s
			}
			var date string
			if err := json.Unmarshal(val, &date); err == nil {
				s.date = date
			}
		case strings.HasPrefix(key, "session_"):
			id := strings.TrimPrefix(key, "session_")
			var turns []locomoTurn
			if err := json.Unmarshal(val, &turns); err != nil {
				// Some entries may be objects (rare); skip.
				continue
			}
			s := accum[id]
			if s == nil {
				s = &sessionAccum{}
				accum[id] = s
			}
			s.turns = turns
		}
	}
	out := make([]locomoSession, 0, len(accum))
	for id, s := range accum {
		out = append(out, locomoSession{ID: id, Date: s.date, Turns: s.turns})
	}
	sort.Slice(out, func(i, j int) bool {
		ii, _ := strconv.Atoi(out[i].ID)
		jj, _ := strconv.Atoi(out[j].ID)
		return ii < jj
	})
	return out, nil
}

var diaIDRE = regexp.MustCompile(`(?i)^d(\d+):\d+$`)

func sessionIDFromDiaID(dia string) (string, bool) {
	m := diaIDRE.FindStringSubmatch(strings.TrimSpace(dia))
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func truncateQ(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeLoCoMoMarkdown(path string, s locomoSummary) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# LoCoMo run — %s\n\n", s.Dataset)
	fmt.Fprintf(b, "Records: %d  ·  Questions scored: %d (abstention skipped: %d)  ·  top-K: %d\n\n",
		s.Records, s.ScoredQuestions, s.AbstentionCount, s.TopK)
	fmt.Fprintf(b, "**Turn-grain retrieval (matches the evidence dia_id directly):**\n\n")
	fmt.Fprintf(b, "- R@5  = %.4f\n- R@10 = %.4f\n- MRR  = %.4f\n\n",
		s.OverallRecall5, s.OverallRecall10, s.MeanReciprocalRank)
	fmt.Fprintf(b, "**Session-grain retrieval (the right session appears in top-K):**\n\n")
	fmt.Fprintf(b, "- R@5  = %.4f\n- R@10 = %.4f\n\n", s.SessionRecall5, s.SessionRecall10)
	fmt.Fprintf(b, "**Per category:**\n\n")
	fmt.Fprintln(b, "| category | n | turn R@5 | turn R@10 | sess R@5 | sess R@10 |")
	fmt.Fprintln(b, "|---|---|---|---|---|---|")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(b, "| %s | %d | %.4f | %.4f | %.4f | %.4f |\n",
			k, c.Count, c.Recall5, c.Recall10, c.SessionRecall5, c.SessionRecall10)
	}
	fmt.Fprintf(b, "\nLatency p50 = %.3fs, p95 = %.3fs (search only; ingest excluded)\n",
		s.LatencySecondsP50, s.LatencySecondsP95)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func printLoCoMoSummary(w io.Writer, s locomoSummary) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "dataset:                %s\n", s.Dataset)
	fmt.Fprintf(w, "records:                %d\n", s.Records)
	fmt.Fprintf(w, "scored questions:       %d\n", s.ScoredQuestions)
	fmt.Fprintf(w, "abstention skipped:     %d\n", s.AbstentionCount)
	fmt.Fprintf(w, "top_k:                  %d\n", s.TopK)
	fmt.Fprintf(w, "turn  recall@5:         %.4f\n", s.OverallRecall5)
	fmt.Fprintf(w, "turn  recall@10:        %.4f\n", s.OverallRecall10)
	fmt.Fprintf(w, "sess  recall@5:         %.4f\n", s.SessionRecall5)
	fmt.Fprintf(w, "sess  recall@10:        %.4f\n", s.SessionRecall10)
	fmt.Fprintf(w, "mean reciprocal rank:   %.4f\n", s.MeanReciprocalRank)
	fmt.Fprintf(w, "latency p50:            %.3fs\n", s.LatencySecondsP50)
	fmt.Fprintf(w, "latency p95:            %.3fs\n", s.LatencySecondsP95)
	fmt.Fprintln(w, "---")
	keys := make([]string, 0, len(s.ByCategory))
	for k := range s.ByCategory {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.ByCategory[k]
		fmt.Fprintf(w, "%-26s n=%4d turn-r@5=%.4f turn-r@10=%.4f sess-r@5=%.4f sess-r@10=%.4f\n",
			k, c.Count, c.Recall5, c.Recall10, c.SessionRecall5, c.SessionRecall10)
	}
}
