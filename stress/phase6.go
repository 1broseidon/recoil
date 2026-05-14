package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

// cmdPhase6 measures search/wake latency at 1k/10k/50k seeded memories.
// Seeds in-process via store.AddMemory to avoid 50k+ subprocess invocations.
// Latency itself is measured via the CLI binary so we capture full overhead.
func cmdPhase6(args []string) {
	fs := flag.NewFlagSet("phase6", flag.ExitOnError)
	sizes := fs.String("sizes", "1000,10000,50000", "comma-separated trial sizes")
	iters := fs.Int("iters", 7, "iterations per query")
	if err := fs.Parse(args); err != nil {
		must(err, "parse phase6 flags")
	}
	_, root := loadManifest()
	parsed := []int{}
	for _, s := range splitCSV(*sizes) {
		n := 0
		fmt.Sscanf(s, "%d", &n)
		if n > 0 {
			parsed = append(parsed, n)
		}
	}

	topics := []string{"caching", "auth", "sqlite", "embeddings", "retrieval", "indexing", "memory", "scope", "session", "fts", "ranking", "mining", "config", "schema", "migration"}
	roles := []string{"note", "decision", "preference", "adr", "constraint", "rule"}

	type trial struct {
		Size      int                           `json:"size"`
		SeedSecs  float64                       `json:"seed_seconds"`
		DBBytes   int64                         `json:"db_bytes"`
		DBMB      float64                       `json:"db_mb"`
		LatencyMS map[string]map[string]float64 `json:"latency_ms"`
	}
	summary := struct {
		Phase  int     `json:"phase"`
		Trials []trial `json:"trials"`
	}{Phase: 6}

	for _, size := range parsed {
		fmt.Fprintf(os.Stderr, "[phase6] seeding %d memories in-process...\n", size)
		tmp, err := os.MkdirTemp("", "recoil-phase6-")
		must(err, "mkdtemp")
		work := filepath.Join(tmp, "work")
		must(os.MkdirAll(work, 0o755), "mkdir work")
		db := filepath.Join(tmp, "recoil.db")

		// Initialize via CLI so all migrations run as in production.
		mustRun(recoilCmd(db, work, "init"))

		st, err := store.Open(db)
		must(err, "store open")
		ctx := context.Background()
		seedStart := time.Now()
		for i := 0; i < size; i++ {
			topic := topics[i%len(topics)]
			role := roles[i%len(roles)]
			_, _, err := st.AddMemory(ctx, store.AddMemoryParams{
				Role:      role,
				Content:   fmt.Sprintf("Memory #%d about %s: synthetic record for latency testing covering %s behavior at index %d.", i, topic, topic, i),
				ScopeKind: "project",
				ScopeID:   "phase6",
				ClaimKey:  fmt.Sprintf("latency.%s.%d", topic, i),
			})
			must(err, "add memory")
		}
		seedElapsed := time.Since(seedStart).Seconds()
		_ = st.Close()
		info, _ := os.Stat(db)
		dbBytes := int64(0)
		if info != nil {
			dbBytes = info.Size()
		}
		// Init a project marker in work so search/wake resolve to the seeded scope.
		// We already seeded with ScopeKind=project ScopeID=phase6, but recoil CLI
		// resolves scope from CWD. Easiest: use --json search via the binary, but
		// supply scope via env. The simplest workaround: re-init a project scope
		// matching "phase6" via a manual project.json.
		projectMarker := filepath.Join(work, ".recoil", "project.json")
		_ = os.MkdirAll(filepath.Dir(projectMarker), 0o700)
		must(os.WriteFile(projectMarker, []byte(`{"id":"phase6"}`), 0o600), "write project marker")

		queries := []struct {
			label string
			args  []string
		}{
			{"search-narrow", []string{"search", "--limit", "5", "caching behavior at index 100"}},
			{"search-broad", []string{"search", "--limit", "5", "memory"}},
			{"wake-no-query", []string{"wake", "--limit", "8"}},
			{"wake-with-query", []string{"wake", "--limit", "8", "indexing"}},
		}
		series := map[string]map[string]float64{}
		for _, q := range queries {
			samples := make([]float64, 0, *iters)
			for j := 0; j < *iters; j++ {
				start := time.Now()
				_, _ = recoilJSON(db, work, append([]string{"--json"}, q.args...)...)
				samples = append(samples, time.Since(start).Seconds())
			}
			sort.Float64s(samples)
			series[q.label] = map[string]float64{
				"n":       float64(len(samples)),
				"p50_ms":  roundFloat(samples[len(samples)/2]*1000, 1),
				"p95_ms":  roundFloat(samples[min(int(float64(len(samples))*0.95), len(samples)-1)]*1000, 1),
				"p99_ms":  roundFloat(samples[len(samples)-1]*1000, 1),
				"mean_ms": roundFloat(meanFloat(samples)*1000, 1),
			}
		}

		summary.Trials = append(summary.Trials, trial{
			Size:      size,
			SeedSecs:  roundFloat(seedElapsed, 1),
			DBBytes:   dbBytes,
			DBMB:      roundFloat(float64(dbBytes)/1024/1024, 2),
			LatencyMS: series,
		})
		fmt.Fprintf(os.Stderr, "[phase6] size=%d db=%.1fMB seed=%.1fs lat=%v\n", size, float64(dbBytes)/1024/1024, seedElapsed, series)
		os.RemoveAll(tmp)
	}

	out := filepath.Join(root, "stress", "reports", "phase6_latency.json")
	must(os.MkdirAll(filepath.Dir(out), 0o755), "mkdir reports")
	data, _ := json.MarshalIndent(summary, "", "  ")
	must(os.WriteFile(out, append(data, '\n'), 0o644), "write phase6")
	emitJSON(map[string]any{"phase": 6, "sizes": parsed, "out": out})
}

func splitCSV(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func meanFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range xs {
		sum += v
	}
	return sum / float64(len(xs))
}
