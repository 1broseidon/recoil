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

// beamFactRecord is one row of the BEAM extracted-facts JSONL.
// "Subject" here is the topic/feature/entity inside the conversation
// (a project component, a requirement, a deadline, a preference) — BEAM
// conversations are project/agent chat traces, so the entities are
// abstract rather than biographical.
type beamFactRecord struct {
	Scale       string   `json:"scale"`
	ConvID      string   `json:"conv_id"`
	BatchNumber int      `json:"batch_number"`
	TimeAnchor  string   `json:"time_anchor"`
	Subject     string   `json:"subject"`
	Predicate   string   `json:"predicate"`
	Object      string   `json:"object"`
	FactText    string   `json:"fact_text"`
	EvidenceIDs []string `json:"evidence_turn_ids"`
}

func runBEAMExtract(args []string) error {
	var dataDir, scale, model, outPath, reasoningEffort string
	var concurrency, maxTokens, limit int
	var verbose bool
	_, err := parseFlags("beam-extract", args, func(fs *flag.FlagSet) {
		fs.StringVar(&dataDir, "data", "", "path to bench/.corpus/beam/<scale>/ (default: derived)")
		fs.StringVar(&scale, "scale", "100K", "BEAM scale: 100K | 500K | 1M | 10M")
		fs.StringVar(&model, "model", "deepseek/deepseek-v4-flash", "extractor model")
		fs.StringVar(&outPath, "out", "", "facts JSONL output path")
		fs.IntVar(&concurrency, "concurrency", 12, "parallel extractor calls")
		fs.IntVar(&maxTokens, "max-tokens", 4000, "extractor max_tokens")
		fs.IntVar(&limit, "limit", 0, "only process first N conversations")
		fs.StringVar(&reasoningEffort, "reasoning-effort", "", "minimal|low|medium|high")
		fs.BoolVar(&verbose, "verbose", false, "verbose per-batch logging")
	})
	if err != nil {
		return err
	}

	if dataDir == "" {
		root, err := corpusRoot()
		if err != nil {
			return err
		}
		dataDir = filepath.Join(root, "beam", scale)
	}
	convDirs, err := listConvDirs(dataDir)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(convDirs) {
		convDirs = convDirs[:limit]
	}

	client, err := NewOpenRouterClient()
	if err != nil {
		return err
	}

	outDir, _ := resultsDir()
	if outPath == "" {
		runID := time.Now().UTC().Format("20060102T150405Z")
		outPath = filepath.Join(outDir, fmt.Sprintf("beam_facts_%s_%s.jsonl", scale, runID))
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	var encMu sync.Mutex

	type job struct {
		convID     string
		batch      beamBatch
		convScale  string
	}
	var jobsList []job
	for _, convDir := range convDirs {
		convID := filepath.Base(convDir)
		batches, err := loadBEAMChat(filepath.Join(convDir, "chat.json"))
		if err != nil {
			return fmt.Errorf("conv %s: %w", convID, err)
		}
		for _, b := range batches {
			if len(b.Turns) == 0 {
				continue
			}
			jobsList = append(jobsList, job{convID: convID, batch: b, convScale: scale})
		}
	}
	fmt.Fprintf(os.Stderr, "BEAM extract: %d batches across %d conversations (model=%s, concurrency=%d)\n",
		len(jobsList), len(convDirs), model, concurrency)

	jobsCh := make(chan job, concurrency*2)
	var wg sync.WaitGroup
	var done int32
	var totalCost float64
	var costMu sync.Mutex
	var factCount int32

	worker := func() {
		defer wg.Done()
		for j := range jobsCh {
			prompt := buildBEAMExtractPrompt(j.batch)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			res, err := client.CompleteWithOptions(ctx, model, []ChatMessage{
				{Role: "system", Content: "You extract structured facts from agent/coding-assistant chat traces. Output strict JSON only."},
				{Role: "user", Content: prompt},
			}, 0.0, maxTokens, CompleteOptions{ReasoningEffort: reasoningEffort})
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s/batch-%d] error: %v\n", j.convID, j.batch.BatchNumber, err)
				atomic.AddInt32(&done, 1)
				continue
			}
			costMu.Lock()
			totalCost += res.CostUSD
			costMu.Unlock()
			facts := parseBEAMExtractedFacts(res.Content, j.convScale, j.convID, j.batch)
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
				fmt.Fprintf(os.Stderr, "  %s/batch-%d: %d facts\n", j.convID, j.batch.BatchNumber, len(facts))
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

func buildBEAMExtractPrompt(b beamBatch) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Batch %d (time_anchor: %s)\n\nConversation turns:\n", b.BatchNumber, b.TimeAnchor)
	for _, group := range b.Turns {
		for _, t := range group {
			content := strings.TrimSpace(t.Content)
			if content == "" {
				continue
			}
			fmt.Fprintf(&sb, "[%d] %s: %s\n", t.ID, t.Role, content)
		}
	}
	sb.WriteString(`
Extract EVERY durable fact stated about the project / agent context discussed in this batch. Be exhaustive — capture user requirements, decisions, preferences, deadlines, feature lists, technical choices, schedules, sprint plans, naming, file paths, tools, libraries, frameworks, error encountered, fixes applied, files changed, sprints/milestones.

For each fact, identify a "subject" that is either:
  - A specific project/feature/component name ("budget tracker", "transactions table", "login feature"), or
  - A user preference / decision category ("deployment timeline", "tooling choice"), or
  - A specific entity like a sprint, deadline, milestone, library, or file path.

Each fact = (subject, predicate, object) as a single English sentence in fact_text. Resolve any relative date phrases against the time_anchor and inline an absolute date in fact_text where applicable.

Output a JSON array. Each element:
  {"subject": "login feature", "predicate": "requires", "object": "username/password fields", "fact_text": "The login feature requires username and password fields (2024-08-12).", "evidence_turn_ids": ["57","58"]}

Skip greetings, off-topic chitchat, hypothetical phrasings, and questions that were never answered. Output JSON array only, no preamble. If nothing factual, output [].`)
	return sb.String()
}

func parseBEAMExtractedFacts(raw, scale, convID string, b beamBatch) []beamFactRecord {
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
		EvidenceIDs []string `json:"evidence_turn_ids"`
	}
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return nil
	}
	out := make([]beamFactRecord, 0, len(parsed))
	for _, p := range parsed {
		text := strings.TrimSpace(p.FactText)
		if text == "" {
			if p.Subject != "" && p.Predicate != "" {
				text = p.Subject + " " + p.Predicate + " " + p.Object
			} else {
				continue
			}
		}
		out = append(out, beamFactRecord{
			Scale:       scale,
			ConvID:      convID,
			BatchNumber: b.BatchNumber,
			TimeAnchor:  b.TimeAnchor,
			Subject:     p.Subject,
			Predicate:   p.Predicate,
			Object:      p.Object,
			FactText:    text,
			EvidenceIDs: p.EvidenceIDs,
		})
	}
	return out
}

func loadBEAMFacts(path string) (map[string][]beamFactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]beamFactRecord{}
	dec := json.NewDecoder(f)
	for dec.More() {
		var rec beamFactRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		out[rec.ConvID] = append(out[rec.ConvID], rec)
	}
	return out, nil
}

// beamProfileRecord is the BEAM analogue of locomoProfileRecord: one
// dense paragraph per topic/subject within a conversation.
type beamProfileRecord struct {
	Scale     string   `json:"scale"`
	ConvID    string   `json:"conv_id"`
	Entity    string   `json:"entity"`
	FactCount int      `json:"fact_count"`
	Profile   string   `json:"profile"`
	Batches   []string `json:"batches"`
}

func runBEAMProfiles(args []string) error {
	var factsPath, outPath string
	var minFacts int
	_, err := parseFlags("beam-profiles", args, func(fs *flag.FlagSet) {
		fs.StringVar(&factsPath, "facts", "", "input BEAM facts JSONL (from beam-extract)")
		fs.StringVar(&outPath, "out", "", "output profiles JSONL")
		fs.IntVar(&minFacts, "min-facts", 2, "minimum facts per subject to emit a profile")
	})
	if err != nil {
		return err
	}
	if factsPath == "" {
		return fmt.Errorf("--facts is required")
	}
	facts, err := loadBEAMFacts(factsPath)
	if err != nil {
		return err
	}

	outDir, _ := resultsDir()
	if outPath == "" {
		runID := time.Now().UTC().Format("20060102T150405Z")
		outPath = filepath.Join(outDir, fmt.Sprintf("beam_profiles_%s.jsonl", runID))
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	total := 0
	for convID, recs := range facts {
		bySubj := map[string][]beamFactRecord{}
		scale := ""
		for _, r := range recs {
			scale = r.Scale
			s := strings.TrimSpace(r.Subject)
			if s == "" {
				continue
			}
			bySubj[s] = append(bySubj[s], r)
		}
		for subj, rows := range bySubj {
			if len(rows) < minFacts {
				continue
			}
			seen := map[string]bool{}
			var lines []string
			batches := map[string]bool{}
			for _, r := range rows {
				line := strings.TrimSpace(r.FactText)
				if line == "" || seen[line] {
					continue
				}
				seen[line] = true
				lines = append(lines, "- "+line)
				batches[fmt.Sprintf("%d", r.BatchNumber)] = true
			}
			batchList := make([]string, 0, len(batches))
			for b := range batches {
				batchList = append(batchList, b)
			}
			profile := fmt.Sprintf("Topic — %s:\n%s", subj, strings.Join(lines, "\n"))
			rec := beamProfileRecord{
				Scale:     scale,
				ConvID:    convID,
				Entity:    subj,
				FactCount: len(rows),
				Profile:   profile,
				Batches:   batchList,
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
			total++
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d topic profiles to %s\n", total, outPath)
	return nil
}

func loadBEAMProfiles(path string) (map[string][]beamProfileRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]beamProfileRecord{}
	dec := json.NewDecoder(f)
	for dec.More() {
		var r beamProfileRecord
		if err := dec.Decode(&r); err != nil {
			return nil, err
		}
		out[r.ConvID] = append(out[r.ConvID], r)
	}
	return out, nil
}
