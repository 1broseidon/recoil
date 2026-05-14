package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdPhase2b runs the universal battery on all repos with session-evidence
// enabled and 6 synthesized transcripts ingested per repo. Compares to the
// Phase 2a default-label run.
func cmdPhase2b(args []string) {
	fs := flag.NewFlagSet("phase2b", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase2b flags")
	}
	m, root := loadManifest()
	type result struct {
		Repo            string  `json:"repo"`
		WithTranscripts float64 `json:"with_transcripts"`
		Default         float64 `json:"default"`
		Delta           float64 `json:"delta_pp"`
		Error           string  `json:"error,omitempty"`
	}
	var rows []result
	for _, r := range m.Repos {
		// Run with transcripts.
		cmd := exec.Command("go", "run", "./stress", "run-repo", r.Key,
			"--label", "with-session-evidence",
			"--config", "with-session-evidence",
			"--with-transcripts",
		)
		cmd.Dir = root
		out, err := cmd.Output()
		row := result{Repo: r.Key}
		if err != nil {
			row.Error = err.Error()
			rows = append(rows, row)
			continue
		}
		var summary struct {
			Accuracy float64 `json:"accuracy"`
		}
		if err := json.Unmarshal(out, &summary); err == nil {
			row.WithTranscripts = summary.Accuracy
		}
		// Look up default report for the same repo.
		dpath := filepath.Join(root, "stress", "reports", fmt.Sprintf("%s__default.json", r.Key))
		if data, err := os.ReadFile(dpath); err == nil {
			var d RepoReport
			if json.Unmarshal(data, &d) == nil {
				row.Default = d.Accuracy
			}
		}
		row.Delta = roundFloat((row.WithTranscripts-row.Default)*100, 2)
		rows = append(rows, row)
	}
	out := filepath.Join(root, "stress", "reports", "phase2b_session_evidence.json")
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")
	data, _ := json.MarshalIndent(map[string]any{"phase": "2b", "rows": rows}, "", "  ")
	must(os.WriteFile(out, append(data, '\n'), 0o644), "write phase2b")
	emitJSON(map[string]any{"phase": "2b", "out": out, "repos": len(rows)})
}
