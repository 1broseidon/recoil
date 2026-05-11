package cmd

import (
	"strings"

	"github.com/1broseidon/recoil/internal/store"
)

func isHistoricalMemory(mem store.Memory) bool {
	switch strings.ToLower(strings.TrimSpace(mem.Validity)) {
	case "historical", "rejected", "superseded", "stale", "tombstoned":
		return true
	default:
		return false
	}
}
