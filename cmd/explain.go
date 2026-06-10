package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/sourcequality"
	"github.com/1broseidon/recoil/internal/store"
)

func explainRetrievalLanes(lanes []retrievalLaneResult, query string, quality sourcequality.Options, ageWindow float64, mode string) []retrievalLaneResult {
	out := make([]retrievalLaneResult, len(lanes))
	for i, lane := range lanes {
		out[i] = lane
		out[i].Results = explainMemories(lane.Results, query, quality, sourcequality.ModeSearch, ageWindow, mode)
	}
	return out
}

func explainWakeLayers(layers []wakeLayer, query string, quality sourcequality.Options, ageWindow float64) []wakeLayer {
	out := make([]wakeLayer, len(layers))
	explainQuery := wakePolicyQuery(query)
	for i, layer := range layers {
		out[i] = layer
		out[i].Memories = explainMemories(layer.Memories, explainQuery, quality, sourcequality.ModeWake, ageWindow, "wake_fts")
	}
	return out
}

func explainMemories(memories []store.Memory, query string, quality sourcequality.Options, mode string, ageWindow float64, retrievalMode string) []store.Memory {
	if ageWindow <= 0 {
		ageWindow = defaultAgingWindowDays
	}
	out := make([]store.Memory, len(memories))
	now := time.Now().UTC()
	for i, mem := range memories {
		out[i] = mem
		out[i].Explain = explainMemory(mem, query, quality, mode, ageWindow, retrievalMode, now)
	}
	return out
}

func explainMemory(mem store.Memory, query string, quality sourcequality.Options, mode string, ageWindow float64, retrievalMode string, now time.Time) []store.ScoreComponent {
	components := []store.ScoreComponent{
		{Name: "final_score", Value: mem.Score, Detail: "score after retrieval and reranking"},
	}
	if strings.TrimSpace(retrievalMode) != "" {
		components = append(components, store.ScoreComponent{Name: "retrieval_mode", Detail: retrievalMode})
	}
	if coverage := signalTokenCoverage(query, mem); coverage > 0 {
		components = append(components, store.ScoreComponent{Name: "token_coverage", Value: coverage, Detail: fmt.Sprintf("%.0f%% of significant query tokens matched", coverage*100)})
	}
	if prior := sourcequality.ScorePriorWithOptions(query, mem.SourcePath, mem.MetadataJSON, mode, quality); prior != 0 {
		components = append(components, store.ScoreComponent{Name: "sourcequality_prior", Value: prior, Detail: mem.SourcePath})
	}
	if penalty := agingPenalty(mem, now, ageWindow); penalty != 0 {
		components = append(components, store.ScoreComponent{Name: "aging_penalty", Value: -penalty, Detail: fmt.Sprintf("role=%s window_days=%.0f", firstNonEmpty(mem.Role, "unknown"), ageWindow)})
	}
	if isGuidanceRole(mem.Role) {
		components = append(components, store.ScoreComponent{Name: "guidance_role_prior", Value: 3.0, Detail: mem.Role})
	}
	if strings.EqualFold(mem.SourceKind, "session_evidence") {
		components = append(components, store.ScoreComponent{Name: "session_evidence_prior", Value: 0.75})
	}
	if strings.EqualFold(mem.SourceKind, "direct") {
		components = append(components, store.ScoreComponent{Name: "direct_memory_prior", Value: 0.5})
	}
	if strings.EqualFold(mem.SourceKind, "file") {
		components = append(components, store.ScoreComponent{Name: "file_chunk_penalty", Value: -0.25})
	}
	if strings.TrimSpace(mem.Why) != "" {
		components = append(components, store.ScoreComponent{Name: "why", Detail: mem.Why})
	}
	return components
}

func renderExplainComponents(b *strings.Builder, components []store.ScoreComponent) {
	if len(components) == 0 {
		return
	}
	b.WriteString("explain:\n")
	for _, component := range components {
		name := strings.TrimSpace(component.Name)
		if name == "" {
			continue
		}
		if component.Detail != "" && (component.Value != 0 || name == "final_score") {
			fmt.Fprintf(b, "  - %s: %.4f (%s)\n", name, component.Value, component.Detail)
		} else if component.Detail != "" {
			fmt.Fprintf(b, "  - %s: %s\n", name, component.Detail)
		} else {
			fmt.Fprintf(b, "  - %s: %.4f\n", name, component.Value)
		}
	}
}
