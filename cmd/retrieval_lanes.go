package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

type retrievalLaneResult struct {
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Results []store.Memory `json:"results"`
}

type searchResult struct {
	Query            string                 `json:"query"`
	Scope            string                 `json:"scope"`
	ScopeID          string                 `json:"scope_id"`
	RetrievalMode    string                 `json:"retrieval_mode"`
	ResultCount      int                    `json:"result_count"`
	HistoryCount     int                    `json:"history_count"`
	ChannelFreshness channelFreshnessResult `json:"channel_freshness"`
	Lanes            []retrievalLaneResult  `json:"lanes"`
	Results          []store.Memory         `json:"results"`
}

func structuredRetrievalLanes(query string, current, historical []store.Memory) []retrievalLaneResult {
	laneDefs := []retrievalLaneResult{
		{Key: "current_decisions", Title: "Current Decisions"},
		{Key: "remote_artifacts", Title: "Peer Memory"},
		{Key: "project_docs", Title: "Project Docs"},
		{Key: "recent_evidence", Title: "Recent Evidence"},
		{Key: "historical", Title: "Historical"},
	}
	add := func(mem store.Memory, historical bool) {
		if broken, reason := deterministicPredicateBroken(mem); broken {
			mem.Why = reason
			laneDefs[4].Results = append(laneDefs[4].Results, mem)
			return
		}
		mem.Why = whyMemorySurfaced(mem, query, strings.TrimSpace(query) != "", historical)
		index := retrievalLaneIndex(mem, historical)
		laneDefs[index].Results = append(laneDefs[index].Results, mem)
	}
	for _, mem := range current {
		add(mem, false)
	}
	for _, mem := range historical {
		add(mem, true)
	}
	out := make([]retrievalLaneResult, 0, len(laneDefs))
	for _, lane := range laneDefs {
		if len(lane.Results) > 0 {
			out = append(out, lane)
		}
	}
	return out
}

func retrievalLaneIndex(mem store.Memory, historical bool) int {
	if historical || isHistoricalMemory(mem) {
		return 4
	}
	if strings.EqualFold(mem.SourceKind, "remote_artifact") {
		return 1
	}
	if strings.EqualFold(mem.SourceKind, "file") {
		return 2
	}
	if strings.EqualFold(mem.SourceKind, "session_evidence") {
		return 3
	}
	if isDecisionLaneRole(mem.Role) {
		return 0
	}
	return 3
}

func retrievalLaneBlocks(lanes []retrievalLaneResult, maxChars int) string {
	var b strings.Builder
	if len(lanes) == 0 {
		return "No current memories found."
	}
	remaining := maxChars
	for _, lane := range lanes {
		if len(lane.Results) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## %s\n\n", lane.Title)
		body := memoryBlocks(lane.Results, remaining, true)
		b.WriteString(body)
		if maxChars > 0 {
			remaining -= len(body)
			if remaining <= 0 {
				break
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func whyMemorySurfaced(mem store.Memory, query string, fromQuery, historical bool) string {
	if broken, reason := deterministicPredicateBroken(mem); broken {
		return reason
	}
	base := ""
	if historical || isHistoricalMemory(mem) {
		base = "matched query but lifecycle marks it historical, stale, rejected, or superseded"
	} else if strings.EqualFold(mem.SourceKind, "remote_artifact") {
		base = "shared by " + firstNonEmpty(mem.SourceAgent, "another agent")
	} else if strings.EqualFold(mem.SourceKind, "file") {
		base = "project document chunk from " + firstNonEmpty(mem.SourcePath, "a mined source")
	} else if strings.EqualFold(mem.SourceKind, "session_evidence") {
		base = "recent selected session evidence (not current truth)"
	} else if isDecisionLaneRole(mem.Role) {
		if mem.ClaimKey != "" {
			base = "current " + firstNonEmpty(mem.Role, "guidance") + " with claim_key " + mem.ClaimKey
		} else {
			base = "current " + firstNonEmpty(mem.Role, "guidance") + " memory"
		}
	} else if fromQuery && strings.TrimSpace(query) != "" {
		base = "matched query terms"
	} else {
		base = "recent current memory in this scope"
	}
	return base + agingWhySuffix(mem, time.Now().UTC(), defaultAgingWindowDays)
}

func isDecisionLaneRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "adr", "decision", "constraint", "preference", "rule":
		return true
	default:
		return false
	}
}
