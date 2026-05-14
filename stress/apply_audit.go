package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
)

// cmdApplyAudit takes the latest audit__ground_truth.json and overwrites
// manifest.json's ground_truth fields with the discovered paths.
func cmdApplyAudit(args []string) {
	fs := flag.NewFlagSet("apply-audit", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		must(err, "parse apply-audit flags")
	}
	root := repoRoot()
	auditPath := filepath.Join(root, "stress", "reports", "audit__ground_truth.json")
	manifestPath := filepath.Join(root, "stress", "manifest.json")

	auditData, err := os.ReadFile(auditPath)
	must(err, "read audit")
	var audit struct {
		Repos []struct {
			Key         string              `json:"key"`
			GroundTruth map[string][]string `json:"ground_truth"`
		} `json:"repos"`
	}
	must(json.Unmarshal(auditData, &audit), "parse audit")

	manifestData, err := os.ReadFile(manifestPath)
	must(err, "read manifest")
	var raw map[string]any
	must(json.Unmarshal(manifestData, &raw), "parse manifest")
	repos, _ := raw["repos"].([]any)

	auditMap := map[string]map[string][]string{}
	for _, a := range audit.Repos {
		auditMap[a.Key] = a.GroundTruth
	}

	for i, r := range repos {
		rm, _ := r.(map[string]any)
		key, _ := rm["key"].(string)
		if gt, ok := auditMap[key]; ok {
			gtAny := map[string]any{}
			for k, v := range gt {
				arr := make([]any, len(v))
				for j, x := range v {
					arr[j] = x
				}
				gtAny[k] = arr
			}
			rm["ground_truth"] = gtAny
			repos[i] = rm
		}
	}
	raw["repos"] = repos
	out, err := json.MarshalIndent(raw, "", "  ")
	must(err, "marshal manifest")
	must(os.WriteFile(manifestPath, append(out, '\n'), 0o644), "write manifest")
	emitJSON(map[string]any{"updated": manifestPath, "repos": len(audit.Repos)})
}
