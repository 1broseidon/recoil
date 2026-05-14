package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Manifest struct {
	ClonesRoot string `json:"clones_root"`
	Repos      []Repo `json:"repos"`
}

type Repo struct {
	Key             string              `json:"key"`
	URL             string              `json:"url"`
	Ref             string              `json:"ref"`
	SHA             string              `json:"sha"`
	Archetype       string              `json:"archetype"`
	ExpectedClasses []string            `json:"expected_classes"`
	GroundTruth     map[string][]string `json:"ground_truth"`
}

// UnmarshalJSON accepts ground_truth values as either a string or a list of strings.
func (r *Repo) UnmarshalJSON(data []byte) error {
	type rawRepo struct {
		Key             string                     `json:"key"`
		URL             string                     `json:"url"`
		Ref             string                     `json:"ref"`
		SHA             string                     `json:"sha"`
		Archetype       string                     `json:"archetype"`
		ExpectedClasses []string                   `json:"expected_classes"`
		GroundTruth     map[string]json.RawMessage `json:"ground_truth"`
	}
	var raw rawRepo
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Key = raw.Key
	r.URL = raw.URL
	r.Ref = raw.Ref
	r.SHA = raw.SHA
	r.Archetype = raw.Archetype
	r.ExpectedClasses = raw.ExpectedClasses
	r.GroundTruth = map[string][]string{}
	for k, v := range raw.GroundTruth {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			r.GroundTruth[k] = []string{s}
			continue
		}
		var ss []string
		if err := json.Unmarshal(v, &ss); err == nil {
			r.GroundTruth[k] = ss
			continue
		}
	}
	return nil
}

type QueryArchetype struct {
	ID                   string   `json:"id"`
	Category             string   `json:"category"`
	Query                string   `json:"query"`
	ExpectsGroundTruth   []string `json:"expects_ground_truth"`
	MustNotContain       []string `json:"must_not_contain"`
	MustNotContainClass  []string `json:"must_not_contain_class"`
	SkipIfNoGroundTruth  string   `json:"skip_if_no_ground_truth"`
}

type QueryBattery struct {
	Description string           `json:"description"`
	Archetypes  []QueryArchetype `json:"archetypes"`
}

type CaseResult struct {
	ID              string             `json:"id"`
	Category        string             `json:"category"`
	Query           string             `json:"query"`
	Passed          bool               `json:"passed"`
	Skipped         bool               `json:"skipped"`
	Error           string             `json:"error,omitempty"`
	MatchedPath     string             `json:"matched_path"`
	ExpectedPaths   []string           `json:"expected_paths"`
	ForbiddenClass  *ForbiddenHit      `json:"forbidden_class_hit,omitempty"`
	ForbiddenString *ForbiddenHit      `json:"forbidden_string_hit,omitempty"`
	TopPaths        []TopResult        `json:"top_paths"`
	ResultCount     int                `json:"result_count"`
	LatencySeconds  float64            `json:"latency_seconds"`
}

type ForbiddenHit struct {
	Rank       int    `json:"rank"`
	Trigger    string `json:"trigger"`
	SourcePath string `json:"source_path,omitempty"`
}

type TopResult struct {
	Rank       int     `json:"rank"`
	SourcePath string  `json:"source_path"`
	Role       string  `json:"role"`
	SourceKind string  `json:"source_kind"`
	Score      float64 `json:"score"`
}

type RepoReport struct {
	Repo               string         `json:"repo"`
	Archetype          string         `json:"archetype"`
	Config             string         `json:"config,omitempty"`
	Label              string         `json:"label"`
	MineElapsedSeconds float64        `json:"mine_elapsed_seconds"`
	Status             map[string]any `json:"status"`
	TotalCases         int            `json:"total_cases"`
	EligibleCases      int            `json:"eligible_cases"`
	Passed             int            `json:"passed"`
	Failed             int            `json:"failed"`
	Accuracy           float64        `json:"accuracy"`
	SearchLatency      LatencyStats   `json:"search_latency_seconds"`
	Cases              []CaseResult   `json:"cases"`
}

type LatencyStats struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
}

// loadManifest reads stress/manifest.json relative to the repo root.
func loadManifest() (*Manifest, string) {
	root := repoRoot()
	path := filepath.Join(root, "stress", "manifest.json")
	data, err := os.ReadFile(path)
	must(err, "read manifest")
	var m Manifest
	must(json.Unmarshal(data, &m), "parse manifest")
	if strings.HasPrefix(m.ClonesRoot, "~") {
		home, err := os.UserHomeDir()
		must(err, "user home")
		m.ClonesRoot = filepath.Join(home, strings.TrimPrefix(m.ClonesRoot, "~/"))
	}
	if env := os.Getenv("RECOIL_CLONES_ROOT"); env != "" {
		m.ClonesRoot = env
	}
	return &m, root
}

func findRepo(m *Manifest, key string) *Repo {
	for i := range m.Repos {
		if m.Repos[i].Key == key {
			return &m.Repos[i]
		}
	}
	must(fmt.Errorf("unknown repo key: %s", key), "find repo")
	return nil
}

func repoPath(m *Manifest, r *Repo) string {
	return filepath.Join(m.ClonesRoot, r.Key, r.Ref)
}

func loadBattery() *QueryBattery {
	root := repoRoot()
	path := filepath.Join(root, "stress", "queries", "universal.json")
	data, err := os.ReadFile(path)
	must(err, "read battery")
	var b QueryBattery
	must(json.Unmarshal(data, &b), "parse battery")
	return &b
}

func repoRoot() string {
	// Walk up from CWD looking for go.mod with module=github.com/1broseidon/recoil.
	wd, err := os.Getwd()
	must(err, "cwd")
	dir := wd
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			if strings.Contains(string(data), "github.com/1broseidon/recoil") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

func must(err error, ctx string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "stress: %s: %v\n", ctx, err)
		os.Exit(1)
	}
}
