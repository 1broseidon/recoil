package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cmdRunRepo runs the universal battery against one repo under one config.
//
// Usage: go run ./stress run-repo <key> [--config name] [--label name] [--mine-limit N] [--include-hidden] [--with-transcripts]
func cmdRunRepo(args []string) {
	fs := flag.NewFlagSet("run-repo", flag.ExitOnError)
	configName := fs.String("config", "", "config variant name under stress/configs/")
	labelOverride := fs.String("label", "", "report label; defaults to config name or 'default'")
	mineLimit := fs.Int("mine-limit", 0, "cap mined chunks (0 = no limit)")
	includeHidden := fs.Bool("include-hidden", false, "include hidden files")
	withTranscripts := fs.Bool("with-transcripts", false, "synthesize transcripts and ingest as session-evidence")
	if err := fs.Parse(args); err != nil {
		must(err, "parse run-repo flags")
	}
	if fs.NArg() < 1 {
		must(fmt.Errorf("missing repo key"), "run-repo args")
	}
	key := fs.Arg(0)
	label := *labelOverride
	if label == "" {
		if *configName != "" {
			label = *configName
		} else {
			label = "default"
		}
	}

	m, root := loadManifest()
	r := findRepo(m, key)
	clone := repoPath(m, r)
	if _, err := os.Stat(clone); os.IsNotExist(err) {
		must(fmt.Errorf("clone missing: %s. Run stress/clone.sh first", clone), "")
	}
	battery := loadBattery()

	tmp, err := os.MkdirTemp("", fmt.Sprintf("recoil-stress-%s-", key))
	must(err, "mkdtemp")
	defer os.RemoveAll(tmp)
	db := filepath.Join(tmp, "recoil.db")

	// Wipe any prior .recoil from the clone, then init, apply config, mine, query.
	cloneRecoil := filepath.Join(clone, ".recoil")
	_ = os.RemoveAll(cloneRecoil)
	defer os.RemoveAll(cloneRecoil)

	mustRun(recoilCmd(db, clone, "init"))
	applyConfig(clone, *configName)

	mineArgs := []string{"mine", "--agent", "stress"}
	if *includeHidden {
		mineArgs = append(mineArgs, "--include-hidden")
	}
	if *mineLimit > 0 {
		mineArgs = append(mineArgs, "--limit", fmt.Sprintf("%d", *mineLimit))
	}
	mineArgs = append(mineArgs, ".")
	mineStart := time.Now()
	mustRun(recoilCmd(db, clone, mineArgs...))
	mineElapsed := time.Since(mineStart).Seconds()

	if *withTranscripts {
		// Ensure session-evidence is enabled regardless of which config was applied.
		mustRun(recoilCmd(db, clone, "config", "set", "session-evidence.enabled", "true"))
		synthDir := filepath.Join(clone, ".recoil-synth")
		_ = os.RemoveAll(synthDir)
		must(os.MkdirAll(synthDir, 0o755), "mkdir synth")
		// Inline synth so we don't shell out to ourselves.
		st := extractRepoStrings(clone)
		for i := 0; i < 6; i++ {
			turns := template(i, r.Key, st)
			sid := fmt.Sprintf("synth-%s-%d", r.Key, i)
			payload := map[string]any{"session_id": sid, "turns": turns}
			data, _ := json.MarshalIndent(payload, "", "  ")
			path := filepath.Join(synthDir, sid+".json")
			must(os.WriteFile(path, append(data, '\n'), 0o644), "write synth")
			mustRun(recoilCmd(db, clone, "session-evidence", "ingest", "--file", path, "--force", "--agent", "stress-synth", "--session-id", sid))
		}
	}

	status, _ := recoilJSON(db, clone, "--json", "status")
	report := RepoReport{
		Repo:               r.Key,
		Archetype:          r.Archetype,
		Config:             *configName,
		Label:              label,
		MineElapsedSeconds: roundFloat(mineElapsed, 3),
		Status:             status,
	}

	var latencies []float64
	for _, c := range battery.Archetypes {
		start := time.Now()
		payload, runErr := recoilJSON(db, clone, "--json", "search", "--limit", "5", c.Query)
		latency := time.Since(start).Seconds()
		if runErr != nil {
			report.Cases = append(report.Cases, CaseResult{
				ID: c.ID, Category: c.Category, Query: c.Query,
				Error: runErr.Error(),
			})
			continue
		}
		latencies = append(latencies, latency)
		rows := extractRows(payload)
		cr := evaluateCase(c, rows, r.GroundTruth)
		cr.LatencySeconds = roundFloat(latency, 4)
		report.Cases = append(report.Cases, cr)
	}

	eligible := 0
	passed := 0
	for _, c := range report.Cases {
		if c.Skipped || c.Error != "" {
			continue
		}
		eligible++
		if c.Passed {
			passed++
		}
	}
	report.TotalCases = len(report.Cases)
	report.EligibleCases = eligible
	report.Passed = passed
	report.Failed = eligible - passed
	if eligible > 0 {
		report.Accuracy = roundFloat(float64(passed)/float64(eligible), 4)
	}
	report.SearchLatency = computeLatency(latencies)

	out := filepath.Join(root, "stress", "reports", fmt.Sprintf("%s__%s.json", r.Key, label))
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")
	data, err := json.MarshalIndent(report, "", "  ")
	must(err, "marshal report")
	must(os.WriteFile(out, append(data, '\n'), 0o644), "write report")

	summary := map[string]any{
		"repo":                 r.Key,
		"label":                label,
		"accuracy":             report.Accuracy,
		"passed":               passed,
		"eligible":             eligible,
		"mine_elapsed_seconds": report.MineElapsedSeconds,
		"report":               out,
	}
	emitJSON(summary)
}

func applyConfig(clone, configName string) {
	cfgDir := filepath.Join(clone, ".recoil")
	must(os.MkdirAll(cfgDir, 0o700), "mkdir .recoil")
	cfgPath := filepath.Join(cfgDir, "config.json")
	if configName == "" {
		must(os.WriteFile(cfgPath, []byte("{\"values\":{}}\n"), 0o600), "write empty config")
		return
	}
	src := filepath.Join(repoRoot(), "stress", "configs", configName+".json")
	data, err := os.ReadFile(src)
	must(err, fmt.Sprintf("read config %s", src))
	must(os.WriteFile(cfgPath, data, 0o600), "write config")
}

func evaluateCase(c QueryArchetype, rows []map[string]any, gt map[string][]string) CaseResult {
	expected := make([]string, 0, len(c.ExpectsGroundTruth))
	for _, key := range c.ExpectsGroundTruth {
		for _, v := range gt[key] {
			if v != "" {
				expected = append(expected, strings.ToLower(v))
			}
		}
	}
	skip := c.SkipIfNoGroundTruth != "" && len(gt[c.SkipIfNoGroundTruth]) == 0
	if !skip && len(expected) == 0 {
		skip = true
	}

	cr := CaseResult{
		ID: c.ID, Category: c.Category, Query: c.Query,
		ExpectedPaths: expected,
		ResultCount:   len(rows),
	}
	if skip {
		cr.Skipped = true
		return cr
	}

	// Derive accepted parent directories from expected paths so any sibling doc
	// in the same expected directory passes — important for repos like bazel
	// where the contributor guide is split across many files in site/en/contribute.
	acceptedDirs := map[string]bool{}
	for _, ep := range expected {
		parent := pathParent(ep)
		if parent != "" && parent != "." {
			acceptedDirs[parent] = true
		}
	}
	for i, row := range rows {
		sp := strings.ToLower(getString(row, "source_path"))
		if i < 5 {
			cr.TopPaths = append(cr.TopPaths, TopResult{
				Rank: i + 1, SourcePath: sp,
				Role:       getString(row, "role"),
				SourceKind: getString(row, "source_kind"),
				Score:      getFloat(row, "score"),
			})
		}
		if cr.MatchedPath == "" {
			matched := false
			for _, ep := range expected {
				if sp == ep || strings.HasSuffix(sp, ep) {
					matched = true
					break
				}
			}
			if !matched && len(acceptedDirs) > 0 {
				spParent := pathParent(sp)
				if acceptedDirs[spParent] {
					matched = true
				}
			}
			if matched {
				cr.MatchedPath = sp
			}
		}
		metadata := getString(row, "metadata")
		for _, cls := range c.MustNotContainClass {
			if strings.Contains(metadata, "\"doc_class\":\""+cls+"\"") {
				cr.ForbiddenClass = &ForbiddenHit{Rank: i + 1, Trigger: cls, SourcePath: sp}
				break
			}
		}
		content := strings.ToLower(getString(row, "content"))
		for _, sub := range c.MustNotContain {
			if strings.Contains(content, strings.ToLower(sub)) {
				cr.ForbiddenString = &ForbiddenHit{Rank: i + 1, Trigger: sub}
				break
			}
		}
	}
	cr.Passed = cr.MatchedPath != "" && cr.ForbiddenClass == nil && cr.ForbiddenString == nil
	return cr
}

// recoilCmd builds an *exec.Cmd for `recoil -d <db> <args...>` running in cwd.
func recoilCmd(db, cwd string, args ...string) *exec.Cmd {
	full := append([]string{"-d", db}, args...)
	cmd := exec.Command("recoil", full...)
	cmd.Dir = cwd
	return cmd
}

func mustRun(cmd *exec.Cmd) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "stress: cmd failed: %s\nstderr: %s\n", strings.Join(cmd.Args, " "), stderr.String())
		os.Exit(1)
	}
}

func recoilJSON(db, cwd string, args ...string) (map[string]any, error) {
	cmd := recoilCmd(db, cwd, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("decode: %s (first bytes: %q)", err, head(stdout.String(), 200))
	}
	return out, nil
}

// extractRows pulls the result list out of a recoil --json payload regardless
// of whether it's wrapped {data:{...}} or {data:[...]} or raw.
func extractRows(payload map[string]any) []map[string]any {
	var raw any = payload
	if d, ok := payload["data"]; ok {
		raw = d
	}
	if m, ok := raw.(map[string]any); ok {
		if r, ok := m["results"]; ok {
			raw = r
		}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		switch x := v.(type) {
		case float64:
			return x
		case int:
			return float64(x)
		}
	}
	return 0
}

func computeLatency(samples []float64) LatencyStats {
	if len(samples) == 0 {
		return LatencyStats{}
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	p50 := sorted[len(sorted)/2]
	p95Index := int(float64(len(sorted))*0.95) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= len(sorted) {
		p95Index = len(sorted) - 1
	}
	return LatencyStats{
		Count: len(sorted),
		Mean:  roundFloat(sum/float64(len(sorted)), 4),
		P50:   roundFloat(p50, 4),
		P95:   roundFloat(sorted[p95Index], 4),
	}
}

func roundFloat(f float64, places int) float64 {
	mult := 1.0
	for i := 0; i < places; i++ {
		mult *= 10
	}
	return float64(int64(f*mult+0.5)) / mult
}

func emitJSON(v any) {
	b, err := json.Marshal(v)
	must(err, "emit json")
	fmt.Println(string(b))
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// pathParent returns the immediate parent directory of a slash-separated path,
// or "" if the path is at root (no separator).
func pathParent(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return ""
	}
	return p[:idx]
}
