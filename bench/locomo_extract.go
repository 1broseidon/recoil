package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// locomoFactRecord is one row of the extracted-facts JSONL written by
// `locomo-extract`. Many facts per (sample_id, session_id).
type locomoFactRecord struct {
	SampleID    string   `json:"sample_id"`
	SessionID   string   `json:"session_id"`
	SessionDate string   `json:"session_date"`
	Subject     string   `json:"subject"`
	Predicate   string   `json:"predicate"`
	Object      string   `json:"object"`
	FactText    string   `json:"fact_text"`
	EvidenceIDs []string `json:"evidence_dia_ids"`
}

func runLoCoMoExtract(args []string) error {
	var dataPath, model, outPath, reasoningEffort string
	var concurrency, maxTokens, limit int
	var verbose bool
	fs, err := parseFlags("locomo-extract", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataPath, "data", "", "path to locomo10.json (default bench/.corpus/)")
		fs.StringVar(&model, "model", "deepseek/deepseek-v4-flash", "extractor model")
		fs.StringVar(&outPath, "out", "", "facts JSONL output path")
		fs.IntVar(&concurrency, "concurrency", 16, "parallel extractor calls")
		fs.IntVar(&maxTokens, "max-tokens", 2000, "extractor max_tokens")
		fs.IntVar(&limit, "limit", 0, "only process first N records (0 = all)")
		fs.StringVar(&reasoningEffort, "reasoning-effort", "", "minimal|low|medium|high")
		fs.BoolVar(&verbose, "verbose", false, "verbose per-session logging")
	})
	if err != nil {
		return err
	}
	_ = fs

	resolved := dataPath
	if resolved == "" {
		resolved = filepath.Join("bench/.corpus", defaultLoCoMoFile)
	}
	records, err := loadLoCoMo(resolved)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	outDir, err := resultsDir()
	if err != nil {
		return err
	}
	if outPath == "" {
		runID := time.Now().UTC().Format("20060102T150405Z")
		outPath = filepath.Join(outDir, fmt.Sprintf("locomo_facts_%s.jsonl", runID))
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	var encMu sync.Mutex

	type job struct {
		sampleID string
		sess     locomoSession
	}
	var jobsList []job
	for _, rec := range records {
		sessions, err := parseConversation(rec.Conversation)
		if err != nil {
			return fmt.Errorf("record %s: %w", rec.SampleID, err)
		}
		for _, s := range sessions {
			if len(s.Turns) == 0 {
				continue
			}
			jobsList = append(jobsList, job{sampleID: rec.SampleID, sess: s})
		}
	}
	fmt.Fprintf(os.Stderr, "extracting facts from %d sessions across %d records (model=%s, concurrency=%d)\n",
		len(jobsList), len(records), model, concurrency)

	jobsCh := make(chan job, concurrency*2)
	var wg sync.WaitGroup
	var done int32
	var totalCost float64
	var costMu sync.Mutex
	var factCount int32

	worker := func() {
		defer wg.Done()
		for j := range jobsCh {
			prompt := buildExtractPrompt(j.sess)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			res, err := client.CompleteWithOptions(ctx, model, []ChatMessage{
				{Role: "system", Content: "You extract structured facts from conversation transcripts. Output strict JSON only."},
				{Role: "user", Content: prompt},
			}, 0.0, maxTokens, CompleteOptions{ReasoningEffort: reasoningEffort})
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s/%s] error: %v\n", j.sampleID, j.sess.ID, err)
				atomic.AddInt32(&done, 1)
				continue
			}
			costMu.Lock()
			totalCost += res.CostUSD
			costMu.Unlock()
			facts := parseExtractedFacts(res.Content, j.sampleID, j.sess)
			encMu.Lock()
			for _, f := range facts {
				if err := enc.Encode(f); err != nil {
					fmt.Fprintf(os.Stderr, "write error: %v\n", err)
				}
			}
			encMu.Unlock()
			atomic.AddInt32(&factCount, int32(len(facts)))
			n := atomic.AddInt32(&done, 1)
			if int(n)%10 == 0 || int(n) == len(jobsList) {
				fmt.Fprintf(os.Stderr, "[%4d/%4d] cost=$%.4f facts=%d\n", n, len(jobsList), totalCost, atomic.LoadInt32(&factCount))
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s/%s: %d facts\n", j.sampleID, j.sess.ID, len(facts))
			}
		}
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker()
	}
	for _, j := range jobsList {
		jobsCh <- j
	}
	close(jobsCh)
	wg.Wait()

	fmt.Fprintf(os.Stderr, "\nwrote %s\n", outPath)
	fmt.Fprintf(os.Stderr, "total facts: %d\n", atomic.LoadInt32(&factCount))
	fmt.Fprintf(os.Stderr, "extractor cost: $%.4f\n", totalCost)
	return nil
}

func buildExtractPrompt(s locomoSession) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Session ID: %s\nSession date: %s\n\n", s.ID, s.Date)
	b.WriteString("Conversation turns:\n")
	for _, t := range s.Turns {
		fmt.Fprintf(&b, "[%s] %s: %s\n", t.DiaID, t.Speaker, strings.TrimSpace(t.Text))
	}
	b.WriteString(`
Extract EVERY piece of personal information mentioned about each named entity (a specific person, place, organisation, project, or pet). Be exhaustive — capture even details that seem minor. The goal is a complete biographical index.

Include without filtering: relationship status, marital status, family members, where they live, where they're from, where they moved from, their occupation, what they study, what they research, hobbies, beliefs, opinions, plans, recent events they attended, things they own, pets, favourites, dislikes, fears, achievements, health conditions, financial state, schedule, career path. If a fact is stated as "yesterday" / "last year" / "two months ago", resolve it against the session date and write the absolute date in the fact_text.

Each fact must be a single English sentence and include subject + predicate + object fields. ALWAYS include facts about relationships, residence, origin, and career — even if they are stated briefly or in passing.

Output a JSON array. Each element:
  {"subject": "Caroline", "predicate": "researches", "object": "adoption agencies", "fact_text": "Caroline researches adoption agencies (2023-06-09).", "evidence_dia_ids": ["D1:23"]}

Skip greetings, hypotheticals, and pure questions. Output JSON array only, no preamble. If genuinely no facts, output [].`)
	return b.String()
}

// parseExtractedFacts strips ```json fences if present and parses the array.
func parseExtractedFacts(raw, sampleID string, sess locomoSession) []locomoFactRecord {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if start := strings.Index(s, "["); start > 0 {
		s = s[start:]
	}
	if end := strings.LastIndex(s, "]"); end > 0 && end < len(s)-1 {
		s = s[:end+1]
	}
	var parsed []struct {
		Subject     string   `json:"subject"`
		Predicate   string   `json:"predicate"`
		Object      string   `json:"object"`
		FactText    string   `json:"fact_text"`
		EvidenceIDs []string `json:"evidence_dia_ids"`
	}
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return nil
	}
	out := make([]locomoFactRecord, 0, len(parsed))
	for _, p := range parsed {
		if strings.TrimSpace(p.FactText) == "" {
			if p.Subject != "" && p.Predicate != "" {
				p.FactText = p.Subject + " " + p.Predicate + " " + p.Object
			} else {
				continue
			}
		}
		out = append(out, locomoFactRecord{
			SampleID:    sampleID,
			SessionID:   sess.ID,
			SessionDate: sess.Date,
			Subject:     p.Subject,
			Predicate:   p.Predicate,
			Object:      p.Object,
			FactText:    p.FactText,
			EvidenceIDs: p.EvidenceIDs,
		})
	}
	return out
}

// loadLoCoMoFacts is used by locomo-qa --facts to inject extracted facts as
// additional memories alongside the raw turns.
func loadLoCoMoFacts(path string) (map[string][]locomoFactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]locomoFactRecord{}
	dec := json.NewDecoder(f)
	for dec.More() {
		var rec locomoFactRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		out[rec.SampleID] = append(out[rec.SampleID], rec)
	}
	return out, nil
}
