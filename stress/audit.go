package main

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cmdAudit walks each clone and emits suggested ground-truth paths per key,
// using filename patterns rather than exact paths. Writes
// stress/reports/audit__<repo>.json and a combined manifest.audited.json.
func cmdAudit(args []string) {
	fs_ := flag.NewFlagSet("audit", flag.ExitOnError)
	if err := fs_.Parse(args); err != nil {
		must(err, "parse audit flags")
	}
	m, root := loadManifest()
	outDir := filepath.Join(root, "stress", "reports")
	must(os.MkdirAll(outDir, 0o755), "mkdir reports")

	type audited struct {
		Key         string              `json:"key"`
		URL         string              `json:"url"`
		Ref         string              `json:"ref"`
		SHA         string              `json:"sha"`
		Archetype   string              `json:"archetype"`
		ExpectedClasses []string        `json:"expected_classes"`
		GroundTruth map[string][]string `json:"ground_truth"`
	}

	var allAudited []audited
	for _, r := range m.Repos {
		clone := repoPath(m, &r)
		if _, err := os.Stat(clone); err != nil {
			continue
		}
		gt := discoverGroundTruth(clone)
		allAudited = append(allAudited, audited{
			Key:             r.Key,
			URL:             r.URL,
			Ref:             r.Ref,
			SHA:             r.SHA,
			Archetype:       r.Archetype,
			ExpectedClasses: r.ExpectedClasses,
			GroundTruth:     gt,
		})
	}
	out := filepath.Join(outDir, "audit__ground_truth.json")
	data, _ := json.MarshalIndent(map[string]any{"repos": allAudited}, "", "  ")
	must(os.WriteFile(out, append(data, '\n'), 0o644), "write audit")
	emitJSON(map[string]any{"out": out, "repos": len(allAudited)})
}

// discoverGroundTruth scans a clone tree and classifies every plausibly
// onboarding-relevant document into one or more ground-truth bins.
//
// Heuristics by filename and directory:
//   - agents.md, claude.md, copilot-instructions.md -> agent_instructions
//   - SECURITY.md, security_policy*, .github/security*, .well-known/security.txt,
//     site/**/security*.md, docs/**/security*.md -> security
//   - CONTRIBUTING.md, CONTRIBUTE*, contributing_*, contribute/**/*.md,
//     contributing/**/*.md, docs/**/contribut*.md, guides/**/contribut*.md,
//     site/**/contribute/**.md -> contributing
//   - DEVELOPERS.md, DEVELOPING.md, developer_guide -> developers
//   - README at root -> root_readme
//
// We cap each bucket at 6 paths to keep ground truth tractable.
func discoverGroundTruth(clone string) map[string][]string {
	// Collect all matches per key, then keep the 6 shallowest paths. This
	// ensures root-level CONTRIBUTING.md beats out 6+ deep `contributing/*/foo.md`
	// when both exist (nextjs has this exact shape).
	collected := map[string][]string{}
	maxDepth := 8 // allow site/en/contribute/foo.md, docs/source/en/contributing.md, etc.
	cloneSlash := filepath.ToSlash(clone)
	must(filepath.WalkDir(clone, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip permission errors
		}
		rel, _ := filepath.Rel(clone, path)
		relSlash := filepath.ToSlash(rel)
		if relSlash == "." {
			return nil
		}
		depth := strings.Count(relSlash, "/")
		if d.IsDir() {
			// Prune noisy directories early.
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != ".github" && name != ".well-known" {
				return filepath.SkipDir
			}
			switch name {
			case "node_modules", "vendor", "third_party", "target", "build", "dist", "out",
				"test", "tests", "testdata", "fixtures", "__fixtures__", "i18n", "translations":
				return filepath.SkipDir
			case "examples", "example":
				if depth > 2 {
					return filepath.SkipDir
				}
			}
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(d.Name())
		ext := strings.ToLower(filepath.Ext(base))
		dirSlash := filepath.ToSlash(filepath.Dir(rel))
		// Only consider doc extensions for ground-truth bins.
		docExt := ext == ".md" || ext == ".markdown" || ext == ".rst" || ext == ".adoc" ||
			ext == ".txt" || ext == ".mdx" || ext == "" // root README without extension
		if !docExt {
			return nil
		}

		add := func(key string) {
			if strings.Contains(dirSlash, "node_modules") ||
				strings.Contains(dirSlash, "vendor/") ||
				strings.Contains(dirSlash, "third_party") ||
				strings.HasPrefix(dirSlash, "i18n") {
				return
			}
			cur := collected[key]
			for _, x := range cur {
				if x == relSlash {
					return
				}
			}
			collected[key] = append(cur, relSlash)
		}

		// Directory-based signals — independent of basename.
		// dirSlash from filepath.Dir(rel) is path-without-trailing-slash, e.g. "site/en/contribute".
		dirHasContribute := strings.HasSuffix(dirSlash, "/contribute") ||
			strings.HasSuffix(dirSlash, "/contributing") ||
			strings.HasSuffix(dirSlash, "/release") ||
			strings.HasSuffix(dirSlash, "/releases") ||
			strings.HasSuffix(dirSlash, "/releasing") ||
			dirSlash == "contribute" || dirSlash == "contributing" ||
			dirSlash == "release" || dirSlash == "releases" || dirSlash == "releasing" ||
			strings.Contains(dirSlash, "/contribute/") ||
			strings.Contains(dirSlash, "/contributing/") ||
			strings.Contains(dirSlash, "/release/") ||
			strings.Contains(dirSlash, "/releases/") ||
			strings.HasPrefix(dirSlash, "contribute/") ||
			strings.HasPrefix(dirSlash, "contributing/") ||
			strings.HasPrefix(dirSlash, "release/") ||
			strings.HasPrefix(dirSlash, "releases/")
		dirHasSecurity := strings.HasSuffix(dirSlash, "/security") ||
			strings.HasSuffix(dirSlash, "/security_policy") ||
			dirSlash == "security" ||
			strings.Contains(dirSlash, "/security/") ||
			strings.HasPrefix(dirSlash, "security/")

		switch {
		case base == "agents.md" || base == "claude.md" || strings.HasPrefix(base, "copilot-instructions"):
			add("agent_instructions")
		case base == "security.md" || base == "security_policy.md" || base == "security_contacts" || base == "security.txt" ||
			base == "security-process.md" || base == "security_process.md":
			add("security")
		case dirHasSecurity:
			add("security")
		case strings.HasPrefix(base, "contributing") || strings.HasPrefix(base, "contribute"):
			add("contributing")
		case dirHasContribute:
			// Any markdown under a contribute/contributing/ directory is a contributing doc.
			add("contributing")
		case strings.HasPrefix(base, "developers") || strings.HasPrefix(base, "developing") || strings.HasPrefix(base, "developer_guide"):
			add("developers")
		case base == "readme.md" || base == "readme" || base == "readme.rst" || base == "readme.adoc":
			if depth == 0 {
				add("root_readme")
			}
		}
		_ = cloneSlash
		return nil
	}), "walk clone")
	gt := map[string][]string{}
	for key, paths := range collected {
		sort.SliceStable(paths, func(i, j int) bool {
			di := strings.Count(paths[i], "/")
			dj := strings.Count(paths[j], "/")
			if di != dj {
				return di < dj
			}
			return paths[i] < paths[j]
		})
		// Cap per parent directory so a single subdir (e.g. site/en/contribute)
		// can't crowd out other plausible answer subtrees (e.g. site/en/release).
		const totalCap = 12
		const perDirCap = 3
		perDir := map[string]int{}
		filtered := make([]string, 0, totalCap)
		for _, p := range paths {
			parent := filepath.Dir(p)
			if perDir[parent] >= perDirCap {
				continue
			}
			perDir[parent]++
			filtered = append(filtered, p)
			if len(filtered) >= totalCap {
				break
			}
		}
		gt[key] = filtered
	}
	return gt
}
