package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// cmdPhase7 runs a 10-query subset on Next.js comparing recoil vs mempalace.
// If mempalace CLI is not installed, the recoil leg still runs and the
// mempalace leg is marked missing.
func cmdPhase7(args []string) {
	fs := flag.NewFlagSet("phase7", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase7 flags")
	}
	_ = fs.Args
	target := "recoil"
	if len(fs.Args()) > 0 {
		target = fs.Arg(0)
	}
	m, root := loadManifest()
	r := findRepo(m, target)
	clone := repoPath(m, r)
	out := filepath.Join(root, "stress", "reports", "phase7_mempalace.json")
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")

	if _, err := os.Stat(clone); err != nil {
		writeJSON(out, map[string]any{"phase": 7, "skipped": true, "reason": fmt.Sprintf("clone missing: %s", clone)})
		emitJSON(map[string]any{"phase": 7, "skipped": true, "out": out})
		return
	}

	subset := []struct {
		ID         string
		Query      string
		PathSuffix []string
	}{
		{"p7-onboard", "how do I set up the development environment", []string{"contributing.md", "readme.md"}},
		{"p7-security", "how do I report a security vulnerability", []string{"security.md"}},
		{"p7-tests", "how do I run the tests", []string{"contributing.md", "readme.md"}},
		{"p7-paraphrase-1", "I am brand new and want to help. Where do I start", []string{"contributing.md", "readme.md"}},
		{"p7-paraphrase-2", "Found a bug. Who do I talk to", []string{"contributing.md", "readme.md"}},
		{"p7-paraphrase-3", "found exploit, who do I email", []string{"security.md"}},
		{"p7-release", "where is the release process documented", []string{"contributing.md", "readme.md"}},
		{"p7-noise", "what is this project about", []string{"readme.md"}},
		{"p7-deep", "what is the contributor guide", []string{"contributing.md"}},
		{"p7-tools", "what testing framework do we use", []string{"contributing.md", "readme.md"}},
	}

	type caseOut struct {
		ID         string   `json:"id"`
		Query      string   `json:"query"`
		Tool       string   `json:"tool"`
		Passed     bool     `json:"passed"`
		TopPaths   []string `json:"top_paths"`
		Suffixes   []string `json:"expects_path_suffixes"`
	}

	// recoil leg.
	tmp, err := os.MkdirTemp("", "recoil-phase7-")
	must(err, "mkdtemp")
	defer os.RemoveAll(tmp)
	db := filepath.Join(tmp, "recoil.db")
	cloneRecoil := filepath.Join(clone, ".recoil")
	_ = os.RemoveAll(cloneRecoil)
	defer os.RemoveAll(cloneRecoil)
	mustRun(recoilCmd(db, clone, "init"))
	mustRun(recoilCmd(db, clone, "mine", "--agent", "phase7", "."))
	var recoilResults []caseOut
	recoilPassed := 0
	for _, c := range subset {
		payload, _ := recoilJSON(db, clone, "--json", "search", "--limit", "5", c.Query)
		rows := extractRows(payload)
		tops := []string{}
		for i, row := range rows {
			if i >= 5 {
				break
			}
			tops = append(tops, strings.ToLower(getString(row, "source_path")))
		}
		matched := false
		for _, tp := range tops {
			for _, suf := range c.PathSuffix {
				if strings.HasSuffix(tp, suf) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			recoilPassed++
		}
		recoilResults = append(recoilResults, caseOut{
			ID: c.ID, Query: c.Query, Tool: "recoil",
			Passed: matched, TopPaths: tops, Suffixes: c.PathSuffix,
		})
	}

	// mempalace leg.
	mpPath, mpOK := lookPath("mempalace")
	mpResults := []caseOut(nil)
	mpPassed := 0
	mpStatus := "missing"
	if mpOK {
		// Init mempalace in a sandboxed home so we don't pollute ~/.mempalace.
		mpHome := filepath.Join(tmp, "mp-home")
		must(os.MkdirAll(mpHome, 0o755), "mkdir mp-home")
		env := append(os.Environ(), "HOME="+mpHome)
		// init: needs auto-mine=false, non-interactive, no-llm so it doesn't try to dial Ollama.
		initCmd := exec.Command("mempalace", "init", clone, "--yes", "--no-llm")
		initCmd.Env = env
		var initStderr bytes.Buffer
		initCmd.Stderr = &initStderr
		if err := initCmd.Run(); err != nil {
			mpStatus = "init-failed"
			mpResults = []caseOut{{ID: "init", Query: "", Tool: "mempalace", Passed: false, TopPaths: []string{initStderr.String()}}}
		} else {
			mineCmd := exec.Command("mempalace", "mine", clone)
			mineCmd.Env = env
			var mineStderr bytes.Buffer
			mineCmd.Stderr = &mineStderr
			if err := mineCmd.Run(); err != nil {
				mpStatus = "mine-failed"
				mpResults = []caseOut{{ID: "mine", Query: "", Tool: "mempalace", Passed: false, TopPaths: []string{mineStderr.String()}}}
			} else {
				mpStatus = "ran"
				for _, c := range subset {
					tops := mempalaceSearch(clone, c.Query, env)
					matched := false
					for _, tp := range tops {
						for _, suf := range c.PathSuffix {
							if strings.HasSuffix(strings.ToLower(tp), suf) {
								matched = true
								break
							}
						}
						if matched {
							break
						}
					}
					if matched {
						mpPassed++
					}
					mpResults = append(mpResults, caseOut{
						ID: c.ID, Query: c.Query, Tool: "mempalace",
						Passed: matched, TopPaths: tops, Suffixes: c.PathSuffix,
					})
				}
			}
		}
	}

	mpAcc := 0.0
	if len(mpResults) > 0 && mpStatus == "ran" {
		mpAcc = float64(mpPassed) / float64(len(subset))
	}
	report := map[string]any{
		"phase":  7,
		"status": mpStatus,
		"mempalace_cli": map[string]any{
			"found": mpOK,
			"path":  mpPath,
		},
		"recoil": map[string]any{
			"passed":   recoilPassed,
			"total":    len(subset),
			"accuracy": roundFloat(float64(recoilPassed)/float64(len(subset)), 4),
			"results":  recoilResults,
		},
		"mempalace": map[string]any{
			"passed":   mpPassed,
			"total":    len(subset),
			"accuracy": roundFloat(mpAcc, 4),
			"results":  mpResults,
		},
	}
	writeJSON(out, report)
	emitJSON(map[string]any{
		"phase":             7,
		"status":            mpStatus,
		"recoil_accuracy":   roundFloat(float64(recoilPassed)/float64(len(subset)), 4),
		"mempalace_accuracy": roundFloat(mpAcc, 4),
		"out":               out,
	})
}

var mempalaceSourceRE = regexp.MustCompile(`(?m)^\s*Source:\s+(.+?)\s*$`)

func mempalaceSearch(dir, query string, env []string) []string {
	cmd := exec.Command("mempalace", "search", query, "--results", "5")
	cmd.Dir = dir
	cmd.Env = env
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout
	_ = cmd.Run()
	matches := mempalaceSourceRE.FindAllStringSubmatch(stdout.String(), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func lookPath(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	// Also check ~/.local/bin since pip --user installs there.
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func writeJSON(path string, payload any) {
	data, err := json.MarshalIndent(payload, "", "  ")
	must(err, "marshal json")
	must(os.WriteFile(path, append(data, '\n'), 0o644), "write json")
}
