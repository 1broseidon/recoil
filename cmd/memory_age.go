package cmd

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/1broseidon/recoil/internal/config"
	"github.com/1broseidon/recoil/internal/store"
)

const defaultAgingWindowDays = 30.0

const (
	recencyPriorMax    = 0.35
	recencyHorizonDays = 90.0
)

func memoryAgeDays(mem store.Memory, now time.Time) float64 {
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(mem.CreatedAt))
	if err != nil {
		return -1
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Sub(created.UTC()).Hours() / 24
}

func agingRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "handoff", "note":
		return true
	default:
		return false
	}
}

// agingPenalty is intentionally mild and bounded: memories inside the freshness
// window pay no penalty; older note/handoff memories pay 0.5 at the window edge
// plus 0.5 for every doubling of age/window, capped at 1.5. Decisions,
// constraints, preferences, rules, ADRs, and sources never age by time alone.
func agingPenalty(mem store.Memory, now time.Time, windowDays float64) float64 {
	if !agingRole(mem.Role) || windowDays <= 0 {
		return 0
	}
	age := memoryAgeDays(mem, now)
	if age < 0 || age <= windowDays {
		return 0
	}
	return math.Min(1.5, 0.5+0.5*math.Log2(age/windowDays))
}

// recencyPrior is a mild, bounded freshness boost for human/session-ingested
// memories where recency often correlates with truth. The log-decay curve starts
// at 0.35 for a memory created now and reaches 0 at 90 days; the 0.35 ceiling is
// intentionally below the 0.75 guidance coverage bonus and role/source priors so
// freshness acts as a tiebreaker, not a dominant ranking signal.
func recencyPrior(mem store.Memory, now time.Time) float64 {
	switch strings.ToLower(strings.TrimSpace(mem.SourceKind)) {
	case "direct", "session_evidence":
	default:
		return 0
	}
	age := memoryAgeDays(mem, now)
	if age < 0 {
		return 0
	}
	decay := math.Log2(1+age) / math.Log2(1+recencyHorizonDays)
	decay = math.Min(1, decay)
	prior := recencyPriorMax * (1 - decay)
	if prior < 0 {
		return 0
	}
	return prior
}

func effectiveAgingWindowDays(settings config.Settings) float64 {
	window := settings.Float64("aging.window-days", defaultAgingWindowDays)
	if window <= 0 {
		return defaultAgingWindowDays
	}
	return window
}

func agingWhySuffix(mem store.Memory, now time.Time, windowDays float64) string {
	if !agingRole(mem.Role) || agingPenalty(mem, now, windowDays) <= 0 {
		return ""
	}
	age := memoryAgeDays(mem, now)
	if age < 0 {
		return ""
	}
	return fmt.Sprintf("; aging — written %.0f days ago, never reaffirmed", math.Floor(age))
}

func deterministicPredicateBroken(mem store.Memory) (bool, string) {
	pred, ok := predicateFromMemory(mem)
	if !ok || pred.Kind != "valid_until" {
		return false, ""
	}
	eval := evaluateValidUntil(pred)
	if eval.Status != "broken" {
		return false, ""
	}
	reason := "valid_until expired — needs recheck"
	if strings.TrimSpace(eval.Prompt) != "" {
		reason += "; " + strings.TrimSpace(eval.Prompt)
	}
	return true, reason
}
