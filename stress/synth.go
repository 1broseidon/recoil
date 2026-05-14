package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// cmdSynth generates deterministic session-evidence transcripts for a repo's clone.
//
// Usage: go run ./stress synth <repo-key> [--n 6] [--seed 42] [--out-dir path]
func cmdSynth(args []string) {
	fs := flag.NewFlagSet("synth", flag.ExitOnError)
	n := fs.Int("n", 6, "number of transcripts")
	seed := fs.Int("seed", 42, "seed for determinism")
	outDir := fs.String("out-dir", "", "default: <clone>/.recoil-synth/")
	if err := fs.Parse(args); err != nil {
		must(err, "parse synth flags")
	}
	if fs.NArg() < 1 {
		must(fmt.Errorf("missing repo key"), "synth args")
	}
	key := fs.Arg(0)
	m, _ := loadManifest()
	r := findRepo(m, key)
	clone := repoPath(m, r)
	if _, err := os.Stat(clone); err != nil {
		must(fmt.Errorf("clone missing: %s", clone), "")
	}
	target := *outDir
	if target == "" {
		target = filepath.Join(clone, ".recoil-synth")
	}
	must(os.MkdirAll(target, 0o755), "mkdir synth")

	st := extractRepoStrings(clone)
	count := *n
	written := 0
	for i := 0; i < count; i++ {
		t := template(i, r.Key, st)
		key := fmt.Sprintf("%s-%d-%d", r.Key, i, *seed)
		h := sha1.Sum([]byte(key))
		sid := fmt.Sprintf("synth-%s-%s", r.Key, hex.EncodeToString(h[:6]))
		payload := map[string]any{
			"session_id": sid,
			"turns":      t,
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		out := filepath.Join(target, sid+".json")
		must(os.WriteFile(out, append(data, '\n'), 0o644), "write transcript")
		written++
	}
	emitJSON(map[string]any{
		"repo":        r.Key,
		"transcripts": written,
		"out_dir":     target,
	})
}

type repoStrings struct {
	Project    string
	Tools      []string
	Files      []string
	Directives []string
}

var (
	codeFenceRE = regexp.MustCompile("`([a-z][a-z0-9_./-]{2,40})`")
	pathRE      = regexp.MustCompile(`[A-Za-z0-9_/.-]+\.(go|py|ts|tsx|md|rs|java|rb|c|h)`)
	headingRE   = regexp.MustCompile(`(?m)^#+\s+(.{4,60})$`)
)

func extractRepoStrings(clone string) repoStrings {
	st := repoStrings{Project: filepath.Base(clone)}
	candidates := []string{"README.md", "README", "AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md", "DEVELOPERS.md"}
	for _, name := range candidates {
		path := filepath.Join(clone, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(data)
		if len(text) > 8000 {
			text = text[:8000]
		}
		for _, m := range codeFenceRE.FindAllStringSubmatch(text, -1) {
			st.Tools = appendUnique(st.Tools, m[1])
		}
		for _, m := range pathRE.FindAllString(text, -1) {
			st.Files = appendUnique(st.Files, m)
		}
		for _, m := range headingRE.FindAllStringSubmatch(text, -1) {
			st.Directives = appendUnique(st.Directives, strings.TrimSpace(m[1]))
		}
	}
	st.Tools = cap8(st.Tools)
	st.Files = cap8(st.Files)
	st.Directives = cap8(st.Directives)
	return st
}

func cap8(s []string) []string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func template(i int, repoKey string, st repoStrings) []map[string]any {
	project := st.Project
	tool := fallback(st.Tools, 0, "the build")
	secondTool := fallback(st.Tools, 1, "the linter")
	fileA := fallback(st.Files, 0, "README.md")
	fileB := fallback(st.Files, 1, "CONTRIBUTING.md")
	heading := fallback(st.Directives, 0, project)

	templates := [][]struct{ role, content string }{
		// Personal-fact / preference
		{
			{"user", fmt.Sprintf("I prefer working on %s in a clean checkout with %s fully wired up. I enjoy tidy diffs.", project, tool)},
			{"assistant", fmt.Sprintf("Got it — I'll keep that preference in mind when suggesting changes around %s.", fileA)},
			{"user", fmt.Sprintf("My current setup uses %s for the inner loop; please default to that.", secondTool)},
		},
		// Decision / update
		{
			{"user", fmt.Sprintf("We changed direction on %s. We are switching to %s and no longer using legacy paths.", heading, tool)},
			{"assistant", fmt.Sprintf("Understood — I'll treat %s as the current path forward.", tool)},
			{"user", fmt.Sprintf("Correction: also update references in %s when you touch related code.", fileB)},
		},
		// Handoff
		{
			{"user", fmt.Sprintf("Handoff note: the next active task is to land the %s change. Blocker: needs review.", heading)},
			{"assistant", fmt.Sprintf("I'll flag %s as the entry point and %s as the support doc.", fileA, fileB)},
			{"user", fmt.Sprintf("Follow-up: do not change behavior in %s for now.", tool)},
		},
		// Rejected path
		{
			{"user", fmt.Sprintf("Rejected approach: do not use ad-hoc scripts for %s. We tried it last quarter and it broke.", heading)},
			{"assistant", fmt.Sprintf("Noted — sticking to the supported %s workflow.", tool)},
			{"user", fmt.Sprintf("Avoid touching %s as part of this; it's frozen until v-next.", fileA)},
		},
		// Personal fact (project-flavored)
		{
			{"user", fmt.Sprintf("I remember when %s first added %s; I worked on it back then.", project, heading)},
			{"assistant", fmt.Sprintf("That history is helpful — I'll keep historical context when recommending edits to %s.", fileA)},
			{"user", fmt.Sprintf("My role here is maintenance; my preference is to defer feature work.")},
		},
	}
	chosen := templates[i%len(templates)]
	turns := make([]map[string]any, 0, len(chosen))
	for idx, t := range chosen {
		turns = append(turns, map[string]any{
			"turn_index": idx + 1,
			"role":       t.role,
			"content":    t.content,
		})
	}
	return turns
}

func fallback(list []string, i int, def string) string {
	if i < len(list) {
		return list[i]
	}
	return def
}
