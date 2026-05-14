package main

import (
	"encoding/json"
	"fmt"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// locomoProfileRecord is one per-entity profile, synthesised by grouping
// the extracted facts on subject and joining their fact_text lines.
//
// It is what Track C ships into the QA harness as an "entity_profile"
// memory: a dense paragraph that contains everything stated about a single
// named entity, so a single retrieval pulls in the entity's complete
// context rather than chasing facts one-by-one.
type locomoProfileRecord struct {
	SampleID  string   `json:"sample_id"`
	Entity    string   `json:"entity"`
	FactCount int      `json:"fact_count"`
	Profile   string   `json:"profile"`
	Sessions  []string `json:"sessions"`
}

func runLoCoMoProfiles(args []string) error {
	var factsPath, outPath string
	var minFacts int
	_, err := parseFlags("locomo-profiles", args, func(fs *flag.FlagSet) {
		fs.StringVar(&factsPath, "facts", "", "input facts JSONL (from locomo-extract)")
		fs.StringVar(&outPath, "out", "", "output profiles JSONL")
		fs.IntVar(&minFacts, "min-facts", 3, "minimum facts per entity to emit a profile")
	})
	if err != nil {
		return err
	}
	if factsPath == "" {
		return fmt.Errorf("--facts is required")
	}
	facts, err := loadLoCoMoFacts(factsPath)
	if err != nil {
		return err
	}

	outDir, _ := resultsDir()
	if outPath == "" {
		runID := time.Now().UTC().Format("20060102T150405Z")
		outPath = filepath.Join(outDir, fmt.Sprintf("locomo_profiles_%s.jsonl", runID))
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	totalProfiles := 0
	for sampleID, recs := range facts {
		byEntity := map[string][]locomoFactRecord{}
		for _, r := range recs {
			subj := strings.TrimSpace(r.Subject)
			if subj == "" {
				continue
			}
			byEntity[subj] = append(byEntity[subj], r)
		}
		entities := make([]string, 0, len(byEntity))
		for e := range byEntity {
			entities = append(entities, e)
		}
		sort.Strings(entities)
		for _, e := range entities {
			rows := byEntity[e]
			if len(rows) < minFacts {
				continue
			}
			seenLines := map[string]bool{}
			var lines []string
			sessionSet := map[string]bool{}
			for _, r := range rows {
				line := strings.TrimSpace(r.FactText)
				if line == "" || seenLines[line] {
					continue
				}
				seenLines[line] = true
				lines = append(lines, "- "+line)
				sessionSet[r.SessionID] = true
			}
			sessions := make([]string, 0, len(sessionSet))
			for s := range sessionSet {
				sessions = append(sessions, s)
			}
			sort.Strings(sessions)
			profile := fmt.Sprintf("Profile — %s:\n%s", e, strings.Join(lines, "\n"))
			rec := locomoProfileRecord{
				SampleID:  sampleID,
				Entity:    e,
				FactCount: len(rows),
				Profile:   profile,
				Sessions:  sessions,
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
			totalProfiles++
		}
	}
	fmt.Fprintf(os.Stderr, "wrote %d entity profiles to %s\n", totalProfiles, outPath)
	return nil
}

// loadLoCoMoProfiles is consumed by locomo-qa --profiles to inject one
// dense memory per entity.
func loadLoCoMoProfiles(path string) (map[string][]locomoProfileRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]locomoProfileRecord{}
	dec := json.NewDecoder(f)
	for dec.More() {
		var r locomoProfileRecord
		if err := dec.Decode(&r); err != nil {
			return nil, err
		}
		out[r.SampleID] = append(out[r.SampleID], r)
	}
	return out, nil
}
