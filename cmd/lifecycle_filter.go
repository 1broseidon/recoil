package cmd

import (
	"strings"

	"github.com/1broseidon/recoil/internal/store"
)

func splitCurrentHistorical(memories []store.Memory) ([]store.Memory, []store.Memory) {
	current := make([]store.Memory, 0, len(memories))
	historical := make([]store.Memory, 0)
	for _, mem := range memories {
		if isHistoricalMemory(mem) {
			historical = append(historical, mem)
			continue
		}
		current = append(current, mem)
	}
	return current, historical
}

func currentMemories(memories []store.Memory) []store.Memory {
	current, _ := splitCurrentHistorical(memories)
	return current
}

func isHistoricalMemory(mem store.Memory) bool {
	switch strings.ToLower(strings.TrimSpace(mem.Validity)) {
	case "historical", "rejected", "superseded", "stale", "tombstoned":
		return true
	default:
		return false
	}
}

func limitMemories(memories []store.Memory, limit int) []store.Memory {
	if limit <= 0 {
		return memories
	}
	if len(memories) <= limit {
		return memories
	}
	return memories[:limit]
}

func staleAwareFetchLimit(limit int) int {
	if limit <= 0 {
		limit = 5
	}
	fetchLimit := limit * 4
	if fetchLimit < 20 {
		return 20
	}
	if fetchLimit > 100 {
		return 100
	}
	return fetchLimit
}
