package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdAggregate builds the per-label matrix view across all repos.
//
// Usage: go run ./stress aggregate [--label name] [--floor 0.99] [--out path]
func cmdAggregate(args []string) {
	fs := flag.NewFlagSet("aggregate", flag.ExitOnError)
	label := fs.String("label", "default", "report label to aggregate")
	floor := fs.Float64("floor", 0.99, "accuracy floor that defines a blocker")
	out := fs.String("out", "", "output path for aggregate markdown")
	if err := fs.Parse(args); err != nil {
		must(err, "parse aggregate flags")
	}

	m, root := loadManifest()
	reports := make([]*RepoReport, 0, len(m.Repos))
	for _, r := range m.Repos {
		path := filepath.Join(root, "stress", "reports", fmt.Sprintf("%s__%s.json", r.Key, *label))
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rep RepoReport
		must(json.Unmarshal(data, &rep), "parse report")
		reports = append(reports, &rep)
	}
	if len(reports) == 0 {
		must(fmt.Errorf("no reports for label %q", *label), "")
	}

	cats := categoriesOf(reports)
	overallPassed, overallEligible := 0, 0
	for _, r := range reports {
		overallPassed += r.Passed
		overallEligible += r.EligibleCases
	}
	overall := 0.0
	if overallEligible > 0 {
		overall = float64(overallPassed) / float64(overallEligible)
	}

	body := &strings.Builder{}
	fmt.Fprintf(body, "# Stress report — label `%s`\n\n", *label)
	fmt.Fprintf(body, "**Overall accuracy:** %.2f%% (%d/%d)  \n", overall*100, overallPassed, overallEligible)
	fmt.Fprintf(body, "**Floor:** %.0f%%  \n", *floor*100)
	fmt.Fprintf(body, "**Repos run:** %d / %d\n\n", len(reports), len(m.Repos))

	body.WriteString("## Matrix\n\n")
	body.WriteString(renderMatrix(reports, cats))
	body.WriteString("\n\n## Latency\n\n")
	body.WriteString(renderLatency(reports))
	body.WriteString("\n\n## Blockers (< ")
	fmt.Fprintf(body, "%.0f%%)\n\n", *floor*100)
	body.WriteString(renderBlockers(reports, *floor))
	body.WriteString("\n")

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(root, "stress", "reports", fmt.Sprintf("aggregate__%s.md", *label))
	}
	must(os.WriteFile(outPath, []byte(body.String()), 0o644), "write aggregate")
	emitJSON(map[string]any{
		"label":            *label,
		"overall_accuracy": roundFloat(overall, 4),
		"repos":            len(reports),
		"passed":           overallPassed,
		"eligible":         overallEligible,
		"out":              outPath,
	})
}

func categoriesOf(reports []*RepoReport) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range reports {
		for _, c := range r.Cases {
			if !seen[c.Category] {
				seen[c.Category] = true
				out = append(out, c.Category)
			}
		}
	}
	return out
}

type catCounts struct{ passed, eligible, skipped int }

func renderMatrix(reports []*RepoReport, cats []string) string {
	header := []string{"repo", "overall"}
	header = append(header, cats...)
	b := &strings.Builder{}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(header)) + "\n")
	for _, r := range reports {
		row := []string{r.Repo, fmt.Sprintf("%.1f%% (%d/%d)", r.Accuracy*100, r.Passed, r.EligibleCases)}
		per := perCategory(r)
		for _, cat := range cats {
			c, ok := per[cat]
			if !ok || c.eligible == 0 {
				row = append(row, "—")
			} else {
				row = append(row, fmt.Sprintf("%d/%d", c.passed, c.eligible))
			}
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	return b.String()
}

func perCategory(r *RepoReport) map[string]catCounts {
	out := map[string]catCounts{}
	for _, c := range r.Cases {
		entry := out[c.Category]
		if c.Skipped {
			entry.skipped++
		} else {
			entry.eligible++
			if c.Passed {
				entry.passed++
			}
		}
		out[c.Category] = entry
	}
	return out
}

func renderLatency(reports []*RepoReport) string {
	b := &strings.Builder{}
	b.WriteString("| repo | mine s | search p50 ms | search p95 ms |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range reports {
		fmt.Fprintf(b, "| %s | %.1f | %.0f | %.0f |\n",
			r.Repo, r.MineElapsedSeconds, r.SearchLatency.P50*1000, r.SearchLatency.P95*1000)
	}
	return b.String()
}

func renderBlockers(reports []*RepoReport, floor float64) string {
	b := &strings.Builder{}
	any := false
	for _, r := range reports {
		if r.Accuracy >= floor {
			continue
		}
		any = true
		fails := 0
		fmt.Fprintf(b, "### %s — %.1f%%\n\n", r.Repo, r.Accuracy*100)
		for _, c := range r.Cases {
			if c.Skipped || c.Error != "" || c.Passed {
				continue
			}
			fails++
			if fails > 10 {
				continue
			}
			top := []string{}
			for _, t := range c.TopPaths {
				if len(top) >= 3 {
					break
				}
				top = append(top, t.SourcePath)
			}
			fmt.Fprintf(b, "- `%s` (%s) — `%s` → top: %s\n",
				c.ID, c.Category, truncate(c.Query, 60), strings.Join(top, ", "))
		}
		if fails > 10 {
			fmt.Fprintf(b, "- … and %d more\n", fails-10)
		}
		b.WriteString("\n")
	}
	if !any {
		return fmt.Sprintf("_No repos below %.0f%% floor._\n", floor*100)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
