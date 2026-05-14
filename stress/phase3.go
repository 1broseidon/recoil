package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cmdPhase3 runs the universal battery on 4 representative repos under 4 config variants.
func cmdPhase3(args []string) {
	fs := flag.NewFlagSet("phase3", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase3 flags")
	}

	_, root := loadManifest()
	targets := []string{"nextjs", "ruff", "rails", "kubernetes"}
	variants := []struct{ label, config string }{
		{"default", ""},
		{"override-docs", "override-docs"},
		{"penalize-noise", "penalize-noise"},
		{"path-filters", "path-filters"},
	}

	type cell struct {
		Repo     string  `json:"repo"`
		Label    string  `json:"label"`
		Accuracy float64 `json:"accuracy"`
		Passed   int     `json:"passed"`
		Eligible int     `json:"eligible"`
		Error    string  `json:"error,omitempty"`
	}
	var table []cell
	for _, t := range targets {
		for _, v := range variants {
			runArgs := []string{"run", "./stress", "run-repo", t, "--label", v.label}
			if v.config != "" {
				runArgs = append(runArgs, "--config", v.config)
			}
			cmd := exec.Command("go", runArgs...)
			cmd.Dir = root
			out, err := cmd.Output()
			c := cell{Repo: t, Label: v.label}
			if err != nil {
				c.Error = fmt.Sprintf("%s", err)
				table = append(table, c)
				continue
			}
			var summary struct {
				Accuracy float64 `json:"accuracy"`
				Passed   int     `json:"passed"`
				Eligible int     `json:"eligible"`
			}
			if err := json.Unmarshal(out, &summary); err == nil {
				c.Accuracy = summary.Accuracy
				c.Passed = summary.Passed
				c.Eligible = summary.Eligible
			}
			table = append(table, c)
		}
	}

	outDir := filepath.Join(root, "stress", "reports")
	must(os.MkdirAll(outDir, 0o755), "mkdir reports")
	jsonPath := filepath.Join(outDir, "phase3_config.json")
	data, _ := json.MarshalIndent(table, "", "  ")
	must(os.WriteFile(jsonPath, append(data, '\n'), 0o644), "write phase3 json")

	mdPath := filepath.Join(outDir, "phase3_config.md")
	b := &strings.Builder{}
	headers := []string{"repo"}
	for _, v := range variants {
		headers = append(headers, v.label)
	}
	b.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(headers)) + "\n")
	for _, t := range targets {
		row := []string{t}
		for _, v := range variants {
			val := "—"
			for _, c := range table {
				if c.Repo == t && c.Label == v.label {
					if c.Error != "" {
						val = "ERR"
					} else {
						val = fmt.Sprintf("%.1f%%", c.Accuracy*100)
					}
					break
				}
			}
			row = append(row, val)
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	must(os.WriteFile(mdPath, []byte(b.String()), 0o644), "write phase3 md")

	emitJSON(map[string]any{
		"phase":    3,
		"targets":  targets,
		"variants": []string{"default", "override-docs", "penalize-noise", "path-filters"},
		"out":      jsonPath,
	})
}
