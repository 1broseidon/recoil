package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// cmdPhase4 runs workflow/lifecycle stress on the recoil clone.
// Tests: edit-then-remine, delete-then-remine, supersede-current-vs-historical,
// wake session-evidence quota.
func cmdPhase4(args []string) {
	fs := flag.NewFlagSet("phase4", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase4 flags")
	}

	m, root := loadManifest()
	repo := findRepo(m, "recoil")
	base := repoPath(m, repo)
	if _, err := os.Stat(base); err != nil {
		must(fmt.Errorf("missing clone: %s", base), "")
	}

	type subResult struct {
		Test   string         `json:"test"`
		Passed bool           `json:"passed"`
		Detail map[string]any `json:"detail,omitempty"`
	}
	var results []subResult

	// Work in a copy so we can edit/delete freely.
	tmp, err := os.MkdirTemp("", "recoil-phase4-")
	must(err, "mkdtemp")
	defer os.RemoveAll(tmp)
	work := filepath.Join(tmp, "work")
	copyTree(base, work, []string{".recoil", ".recoil-synth"})
	db := filepath.Join(tmp, "recoil.db")
	mustRun(recoilCmd(db, work, "init"))
	mustRun(recoilCmd(db, work, "mine", "--agent", "lifecycle", "."))

	// Test A: edit-then-remine.
	sentinel := "LIFECYCLE-EDIT-SENTINEL-7C3F"
	readme := filepath.Join(work, "README.md")
	orig, err := os.ReadFile(readme)
	must(err, "read README")
	must(os.WriteFile(readme, append(orig, []byte("\n\n## Lifecycle test\n\n"+sentinel+"\n")...), 0o644), "edit README")
	mustRun(recoilCmd(db, work, "mine", "--agent", "lifecycle", "."))
	editPayload, _ := recoilJSON(db, work, "--json", "search", "--limit", "5", sentinel)
	editRows := extractRows(editPayload)
	results = append(results, subResult{
		Test: "edit-then-remine-finds-new-content",
		Passed: len(editRows) >= 1,
		Detail: map[string]any{"found_rows": len(editRows)},
	})

	// Test B: delete-then-remine stales orphans.
	delPath := "bench/RESULTS.md"
	delTarget := filepath.Join(work, delPath)
	if _, err := os.Stat(delTarget); err == nil {
		must(os.Remove(delTarget), "remove bench/RESULTS.md")
		mustRun(recoilCmd(db, work, "mine", "--agent", "lifecycle", "."))
		cur, _ := recoilJSON(db, work, "--json", "list", "--source", delPath, "--current", "--limit", "20")
		hist, _ := recoilJSON(db, work, "--json", "list", "--source", delPath, "--historical", "--limit", "20")
		curRows := extractRows(cur)
		histRows := extractRows(hist)
		results = append(results, subResult{
			Test:   "delete-then-remine-stales-orphans",
			Passed: len(curRows) == 0 && len(histRows) >= 1,
			Detail: map[string]any{"current": len(curRows), "historical": len(histRows)},
		})
	} else {
		results = append(results, subResult{Test: "delete-then-remine-stales-orphans", Passed: false, Detail: map[string]any{"reason": "target missing"}})
	}

	// Test C: decide + supersede.
	mustRun(recoilCmd(db, work, "decide", "--claim-key", "lifecycle.driver", "Use mattn/go-sqlite3 with FTS5"))
	first, _ := recoilJSON(db, work, "--json", "search", "--claim-key", "lifecycle.driver", "--limit", "5", "sqlite driver")
	firstRows := extractRows(first)
	if len(firstRows) > 0 {
		id := getString(firstRows[0], "id")
		if id != "" {
			mustRun(recoilCmd(db, work, "supersede", id, "--claim-key", "lifecycle.driver", "Use modernc/sqlite (no CGO)"))
			cur, _ := recoilJSON(db, work, "--json", "search", "--claim-key", "lifecycle.driver", "--current", "--limit", "5", "sqlite driver")
			curRows := extractRows(cur)
			content := strings.ToLower(joinContents(curRows))
			results = append(results, subResult{
				Test:   "supersede-current-beats-old",
				Passed: strings.Contains(content, "modernc") && !strings.Contains(content, "mattn"),
				Detail: map[string]any{"top_content": truncate(joinContents(curRows[:min(len(curRows), 1)]), 80)},
			})
		}
	} else {
		results = append(results, subResult{Test: "supersede-current-beats-old", Passed: false, Detail: map[string]any{"reason": "decide+search failed"}})
	}

	// Test D: wake session-evidence quota.
	mustRun(recoilCmd(db, work, "config", "set", "session-evidence.enabled", "true"))
	synthDir := filepath.Join(work, ".synth")
	must(os.MkdirAll(synthDir, 0o755), "mkdir synth")
	for i := 0; i < 10; i++ {
		t := map[string]any{
			"session_id": fmt.Sprintf("lifecycle-%d", i),
			"turns": []map[string]any{
				{"turn_index": 1, "role": "user", "content": fmt.Sprintf("Handoff note: next active task is iteration %d of the lifecycle test.", i)},
				{"turn_index": 2, "role": "assistant", "content": "Understood, will keep that as the active task."},
			},
		}
		data, _ := json.Marshal(t)
		path := filepath.Join(synthDir, fmt.Sprintf("sess-%d.json", i))
		must(os.WriteFile(path, data, 0o644), "write synth")
		mustRun(recoilCmd(db, work, "session-evidence", "ingest", "--file", path, "--force", "--agent", "synth", "--session-id", fmt.Sprintf("lifecycle-%d", i)))
	}
	wakePayload, _ := recoilJSON(db, work, "--json", "wake", "--limit", "10")
	layersRaw := getNested(wakePayload, "data", "layers")
	l0SE, l2SE := 0, 0
	if layers, ok := layersRaw.([]any); ok {
		for _, L := range layers {
			lm, _ := L.(map[string]any)
			key := getString(lm, "key")
			rs, _ := lm["results"].([]any)
			for _, r := range rs {
				row, _ := r.(map[string]any)
				if getString(row, "source_kind") != "session_evidence" {
					continue
				}
				if strings.HasPrefix(key, "l0") {
					l0SE++
				}
				if strings.HasPrefix(key, "l2") {
					l2SE++
				}
			}
		}
	}
	results = append(results, subResult{
		Test:   "wake-session-evidence-quota",
		Passed: l0SE <= 1 && l2SE <= 2,
		Detail: map[string]any{"l0_se": l0SE, "l2_se": l2SE},
	})

	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	out := filepath.Join(root, "stress", "reports", "phase4_lifecycle.json")
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")
	data, _ := json.MarshalIndent(map[string]any{"phase": 4, "results": results, "passed": passed, "total": len(results)}, "", "  ")
	must(os.WriteFile(out, append(data, '\n'), 0o644), "write phase4")
	emitJSON(map[string]any{"phase": 4, "passed": passed, "total": len(results), "out": out})
}

func copyTree(src, dst string, skip []string) {
	must(os.MkdirAll(dst, 0o755), "mkdir dst")
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	must(filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		first := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if skipSet[first] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}), "copy tree")
}

func joinContents(rows []map[string]any) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, getString(r, "content"))
	}
	return strings.Join(parts, " ")
}

func getNested(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
