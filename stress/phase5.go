package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
)

// cmdPhase5 runs adversarial cases on a seeded ephemeral DB.
func cmdPhase5(args []string) {
	fs := flag.NewFlagSet("phase5", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase5 flags")
	}
	_, root := loadManifest()

	tmp, err := os.MkdirTemp("", "recoil-phase5-")
	must(err, "mkdtemp")
	defer os.RemoveAll(tmp)
	work := filepath.Join(tmp, "work")
	must(os.MkdirAll(work, 0o755), "mkdir work")
	db := filepath.Join(tmp, "recoil.db")
	mustRun(recoilCmd(db, work, "init"))

	// Seed.
	mustRun(recoilCmd(db, work, "add", "--role", "preference", "Dr. Lee was my dermatologist for the benign biopsy."))
	mustRun(recoilCmd(db, work, "add", "--role", "preference", "Dr. Lim treated my brother for a fractured wrist."))
	mustRun(recoilCmd(db, work, "add", "--role", "note", "Identical placeholder content for path collision A."))
	mustRun(recoilCmd(db, work, "add", "--role", "note", "Identical placeholder content for path collision A."))
	mustRun(recoilCmd(db, work, "add", "--role", "preference", "私は東京で働いています and I love café-grade espresso for the morning ☕."))
	longContent := strings.Repeat("This is a long memory about query latency. ", 200)
	mustRun(recoilCmd(db, work, "add", "--role", "decision", longContent))
	mustRun(recoilCmd(db, work, "decide", "--claim-key", "adv.driver", "Current driver is mattn/go-sqlite3."))
	mustRun(recoilCmd(db, work, "add", "--role", "decision", "--validity", "rejected", "--claim-key", "adv.driver", "Rejected modernc/sqlite (no FTS5 support)."))

	type subResult struct {
		Test   string         `json:"test"`
		Passed bool           `json:"passed"`
		Detail map[string]any `json:"detail,omitempty"`
	}
	var results []subResult

	// A: doctor disambiguation.
	r, _ := recoilJSON(db, work, "--json", "search", "--limit", "5", "Which dermatologist did I see, Dr. Lee or Dr. Lim?")
	rows := extractRows(r)
	content := strings.ToLower(joinContents(rows))
	snippets := make([]string, 0, 3)
	for i, row := range rows {
		if i >= 3 {
			break
		}
		snippets = append(snippets, truncate(getString(row, "content"), 60))
	}
	results = append(results, subResult{
		Test:   "doctor-disambiguation-lee-vs-lim",
		Passed: strings.Contains(content, "lee") && strings.Contains(content, "biopsy"),
		Detail: map[string]any{"snippets": snippets},
	})

	// B: duplicate dedup.
	r, _ = recoilJSON(db, work, "--json", "search", "--limit", "5", "placeholder content path collision")
	rows = extractRows(r)
	unique := map[string]bool{}
	for _, row := range rows {
		unique[getString(row, "content")] = true
	}
	results = append(results, subResult{
		Test:   "duplicate-content-dedup",
		Passed: len(rows) >= 1 && len(rows) <= 2,
		Detail: map[string]any{"row_count": len(rows), "unique_contents": len(unique)},
	})

	// C: unicode search.
	r, _ = recoilJSON(db, work, "--json", "search", "--limit", "5", "café espresso morning")
	rows = extractRows(r)
	hitUnicode := false
	for _, row := range rows {
		c := getString(row, "content")
		if strings.Contains(c, "café") || strings.Contains(c, "東京") {
			hitUnicode = true
			break
		}
	}
	results = append(results, subResult{
		Test:   "unicode-content-search",
		Passed: hitUnicode,
		Detail: map[string]any{"row_count": len(rows)},
	})

	// D: long content intact.
	r, _ = recoilJSON(db, work, "--json", "search", "--limit", "5", "query latency long memory")
	rows = extractRows(r)
	var longMatch map[string]any
	for _, row := range rows {
		if strings.Contains(getString(row, "content"), "long memory about query latency") {
			longMatch = row
			break
		}
	}
	longLen := 0
	if longMatch != nil {
		longLen = len(getString(longMatch, "content"))
	}
	results = append(results, subResult{
		Test:   "long-content-stored-intact",
		Passed: longMatch != nil && longLen > 1000,
		Detail: map[string]any{"content_len": longLen},
	})

	// E: current beats rejected.
	r, _ = recoilJSON(db, work, "--json", "search", "--current", "--limit", "5", "--claim-key", "adv.driver", "sqlite driver")
	rows = extractRows(r)
	content = strings.ToLower(joinContents(rows))
	results = append(results, subResult{
		Test:   "current-beats-rejected",
		Passed: strings.Contains(content, "mattn") && !strings.Contains(content, "rejected modernc"),
		Detail: map[string]any{"contents": contentSnippets(rows, 3)},
	})

	// F: historical surfaces rejected.
	r, _ = recoilJSON(db, work, "--json", "search", "--historical", "--limit", "5", "--claim-key", "adv.driver", "sqlite driver")
	rows = extractRows(r)
	content = strings.ToLower(joinContents(rows))
	results = append(results, subResult{
		Test:   "historical-surfaces-rejected",
		Passed: strings.Contains(content, "rejected modernc"),
		Detail: map[string]any{"contents": contentSnippets(rows, 3)},
	})

	// G: classify.override longest-match precedence.
	cfgPath := filepath.Join(work, ".recoil", "config.json")
	cfg := map[string]any{"values": map[string]any{
		"classify.override.docs/**":          "product_docs",
		"classify.override.docs/security/**": "security",
	}}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	must(os.MkdirAll(filepath.Dir(cfgPath), 0o700), "mkdir cfg")
	must(os.WriteFile(cfgPath, data, 0o600), "write cfg")
	must(os.MkdirAll(filepath.Join(work, "docs", "security"), 0o755), "mkdir docs/security")
	must(os.WriteFile(filepath.Join(work, "docs", "security", "policy.md"), []byte("# Vulnerability disclosure\n\nReport via security@example.com.\n"), 0o644), "write policy")
	must(os.WriteFile(filepath.Join(work, "docs", "intro.md"), []byte("# Intro\n\nWelcome to the project.\n"), 0o644), "write intro")
	mustRun(recoilCmd(db, work, "mine", "--agent", "adv", "."))
	r, _ = recoilJSON(db, work, "--json", "search", "--limit", "5", "vulnerability disclosure policy")
	rows = extractRows(r)
	topPath := ""
	if len(rows) > 0 {
		topPath = strings.ToLower(getString(rows[0], "source_path"))
	}
	results = append(results, subResult{
		Test:   "classify-override-longest-wins",
		Passed: strings.HasSuffix(topPath, "docs/security/policy.md"),
		Detail: map[string]any{"top_path": topPath},
	})

	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	out := filepath.Join(root, "stress", "reports", "phase5_adversarial.json")
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")
	payload, _ := json.MarshalIndent(map[string]any{"phase": 5, "results": results, "passed": passed, "total": len(results)}, "", "  ")
	must(os.WriteFile(out, append(payload, '\n'), 0o644), "write phase5")
	emitJSON(map[string]any{"phase": 5, "passed": passed, "total": len(results), "out": out})
}

func contentSnippets(rows []map[string]any, n int) []string {
	out := make([]string, 0, n)
	for i, row := range rows {
		if i >= n {
			break
		}
		out = append(out, truncate(getString(row, "content"), 50))
	}
	return out
}
