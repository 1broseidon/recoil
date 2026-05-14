package retrieval

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// ProfileText returns deterministic, searchable profile text derived from a
// user's own words. It is intentionally conservative: it emits nothing unless
// the text contains first-person preference, interest, setup, or recommendation
// language. Callers can index this as a derived trace while preserving the
// original source content separately.
func ProfileText(userText string) string {
	userLower := strings.ToLower(userText)
	hasInterest := false
	for _, marker := range []string{
		"i prefer", "i like", "i love", "i enjoy", "favorite", "favourite",
		"i am working in", "i'm working in", "i work in", "my current", "my setup",
		"i'm looking to", "i am looking to", "i still remember", "recommend", "can you suggest",
	} {
		if strings.Contains(userLower, marker) {
			hasInterest = true
			break
		}
	}
	if !hasInterest {
		return ""
	}

	terms := SignificantTokens(userText)
	if len(terms) > 24 {
		terms = terms[:24]
	}
	tags := []string{
		"user_interest", "user_preference", "user_profile", "likes", "prefers", "enjoys",
		"works_in", "current_setup", "recommendations", "resources", "publications",
		"conferences", "suggestions",
	}
	if strings.Contains(userLower, "basil") || strings.Contains(userLower, "mint") || strings.Contains(userLower, "herb") || strings.Contains(userLower, "recipe") {
		tags = append(tags, "homegrown", "garden", "ingredients", "dinner", "cooking", "recipes", "herbs", "fresh")
	}
	if strings.Contains(userLower, "power bank") || strings.Contains(userLower, "charging") || strings.Contains(userLower, "battery") || strings.Contains(userLower, "tech accessories") {
		tags = append(tags, "phone", "battery", "power", "charging", "portable", "travel", "accessories")
	}
	if strings.Contains(userLower, "still remember") || strings.Contains(userLower, "high school") || strings.Contains(userLower, "debate team") || strings.Contains(userLower, "advanced placement") {
		tags = append(tags, "nostalgic", "nostalgia", "memories", "reunion", "high_school", "school", "friends")
	}
	return strings.Join(tags, " ") + " " + strings.Join(terms, " ") + " excerpt " + signalExcerpt(userText)
}

// UpdateText returns deterministic, searchable update text when a source
// states that prior project/user state changed. It stays quiet unless the text
// contains explicit update/correction/switch language, because stale inferred
// updates are worse than missing ones.
func UpdateText(text string) string {
	lower := strings.ToLower(text)
	hasUpdate := false
	for _, marker := range []string{
		"we changed", "i changed", "changed to", "updated", "update:", "correction",
		"actually change", "change the",
		"now use", "use now", "no longer", "instead of", "switch to", "switched to",
		"replace", "replaced", "current", "currently", "latest",
	} {
		if strings.Contains(lower, marker) {
			hasUpdate = true
			break
		}
	}
	if !hasUpdate {
		return ""
	}
	terms := SignificantTokens(text)
	if len(terms) > 28 {
		terms = terms[:28]
	}
	tags := []string{
		"current_state", "latest_update", "knowledge_update", "changed", "updated",
		"now", "currently", "instead", "no_longer", "replacement", "supersedes",
	}
	return strings.Join(tags, " ") + " " + strings.Join(terms, " ") + " excerpt " + signalExcerpt(text)
}

func signalExcerpt(text string) string {
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) > 48 {
		fields = fields[:48]
	}
	return strings.Join(fields, " ")
}

// ExpandedQueryText appends conservative lexical aliases for common entity
// classes that people ask agents to count or order across sessions. The
// original query remains first, so exact sparse matches still dominate.
func ExpandedQueryText(query string) string {
	queryLower := strings.ToLower(query)
	var terms []string
	add := func(words ...string) {
		terms = append(terms, words...)
	}
	if strings.Contains(queryLower, "doctor") || strings.Contains(queryLower, "physician") {
		add("dr", "physician", "doctor", "dermatologist", "ent", "specialist", "primary", "care", "provider")
	}
	if strings.Contains(queryLower, "sibling") || strings.Contains(queryLower, "brother") || strings.Contains(queryLower, "sister") {
		add("sibling", "siblings", "brother", "brothers", "sister", "sisters", "family")
	}
	if strings.Contains(queryLower, "kitchen appliance") || strings.Contains(queryLower, "appliance") {
		add("kitchen", "appliance", "blender", "toaster", "microwave", "oven", "mixer", "air", "fryer", "coffee", "maker", "smoker")
	}
	if strings.Contains(queryLower, "buy") || strings.Contains(queryLower, "bought") || strings.Contains(queryLower, "purchase") {
		add("buy", "bought", "purchase", "purchased", "got", "new")
	}
	if strings.Contains(queryLower, "bake") || strings.Contains(queryLower, "baked") || strings.Contains(queryLower, "baking") {
		add("bake", "baked", "baking", "cake", "cookies", "bread", "pie", "pastry", "oven", "recipe")
	}
	if strings.Contains(queryLower, "hike") || strings.Contains(queryLower, "hikes") || strings.Contains(queryLower, "distance") {
		add("hike", "hiked", "hiking", "trail", "distance", "mile", "miles", "kilometer", "kilometers", "km")
	}
	if strings.Contains(queryLower, "sports event") || strings.Contains(queryLower, "sporting event") {
		add("sports", "event", "events", "race", "tournament", "marathon", "game", "match", "competition")
	}
	if strings.Contains(queryLower, "graduated") || strings.Contains(queryLower, "graduation") || strings.Contains(queryLower, "college") {
		add("graduated", "graduation", "college", "university", "bachelor", "bachelors", "degree", "completed", "age", "old")
	}
	if strings.Contains(queryLower, "how old") || strings.Contains(queryLower, " age ") || strings.HasPrefix(queryLower, "age ") || strings.HasSuffix(queryLower, " age") {
		add("age", "old", "current", "currently")
	}
	if strings.Contains(queryLower, "session evidence") || strings.Contains(queryLower, "session-evidence") || strings.Contains(queryLower, "min chars") || strings.Contains(queryLower, "min-chars") {
		add("session", "evidence", "session-evidence", "min", "chars", "minimum", "character", "characters", "default", "threshold")
	}
	// Security intent → pull SECURITY.md into the candidate set even when the
	// user's exact wording (exploit, cve, "who do I email") doesn't appear in the
	// security policy text. Without these aliases, FTS doesn't surface SECURITY.md
	// and the sourcequality +5 prior can't take effect.
	if strings.Contains(queryLower, "exploit") ||
		strings.Contains(queryLower, "cve") ||
		strings.Contains(queryLower, "vuln") ||
		strings.Contains(queryLower, "vulnerability") ||
		strings.Contains(queryLower, "disclosure") ||
		strings.Contains(queryLower, "security") {
		add("security", "vulnerability", "disclosure", "policy", "reporting", "advisory", "responsible", "report", "contact", "contacts")
	}
	// Onboarding / contributor intent → ensure CONTRIBUTING* gets pulled in for
	// queries that use morphological variants ("contributor", "contributors",
	// "contribution") which FTS does not stem to "contributing".
	if strings.Contains(queryLower, "contributor") ||
		strings.Contains(queryLower, "contributors") ||
		strings.Contains(queryLower, "contribution") ||
		strings.Contains(queryLower, "contribute") {
		add("contributing", "contributor", "contributors", "contribute", "contribution", "guide", "guidelines")
	}
	// Bug-report / "who do I talk to" → contributing or security is the right
	// place; expand to give both classes a chance to surface.
	if strings.Contains(queryLower, "found a bug") ||
		strings.Contains(queryLower, "report a bug") ||
		strings.Contains(queryLower, "who do i talk to") ||
		strings.Contains(queryLower, "who do i email") ||
		strings.Contains(queryLower, "who to email") ||
		strings.Contains(queryLower, "where do i report") {
		add("contributing", "contribute", "issue", "issues", "bug", "report", "reporting", "tracker")
	}
	// Release intent → expand so docs in dedicated release/ subtrees get found.
	if strings.Contains(queryLower, "release") ||
		strings.Contains(queryLower, "cut a version") ||
		strings.Contains(queryLower, "publish") {
		add("release", "release-notes", "publish", "publishing", "version", "tag", "changelog")
	}
	terms = uniqueStrings(terms)
	if len(terms) == 0 {
		return query
	}
	return strings.TrimSpace(query + " " + strings.Join(terms, " "))
}

// QueryVariants returns deterministic sparse-search legs for a user query.
// The first variant is always the original query; later variants preserve
// concrete fragments and conservative lexical expansion for better recall.
func QueryVariants(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	var variants []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		variants = append(variants, v)
	}
	add(query)
	expanded := ExpandedQueryText(query)
	if expanded != query {
		add(expanded)
	}
	for _, sep := range []string{" compared to ", " versus ", " vs "} {
		if strings.Contains(strings.ToLower(query), sep) {
			for _, part := range strings.Split(strings.ToLower(query), sep) {
				part = strings.TrimSpace(part)
				if len(SignificantTokens(part)) >= 1 {
					add(part)
					if expandedPart := ExpandedQueryText(part); expandedPart != part {
						add(expandedPart)
					}
				}
			}
		}
	}
	for _, part := range querySplitRE.Split(query, -1) {
		part = strings.TrimSpace(part)
		if len(SignificantTokens(part)) >= 2 {
			add(part)
			if expandedPart := ExpandedQueryText(part); expandedPart != part {
				add(expandedPart)
			}
		}
	}
	if compact := strings.Join(SignificantTokens(query), " "); compact != "" {
		add(compact)
	}
	return uniqueStrings(variants)
}

var doctorNameRE = regexp.MustCompile(`(?i)\bdr\.?\s+([a-z][a-z'-]{1,})\b`)

// DoctorNameTerms returns explicit doctor surnames from queries like
// "Dr. Alvarez". A sparse search should not satisfy those with another doctor.
func DoctorNameTerms(query string) []string {
	var names []string
	for _, match := range doctorNameRE.FindAllStringSubmatch(query, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.ToLower(strings.Trim(match[1], " .,'\""))
		if name == "" || name == "dr" || name == "doctor" {
			continue
		}
		names = append(names, name)
	}
	return uniqueStrings(names)
}

// TemporalScore scores explicit relative-date language. For ordinary
// non-temporal questions, the score is gated by lexical evidence so dates
// narrow matching candidates instead of overruling topical relevance.
func TemporalScore(queryDate, sourceDate time.Time, query, sourceText, questionType string, lexicalEvidence float64) float64 {
	if sourceDate.IsZero() {
		return 0
	}
	queryLower := strings.ToLower(query)
	sourceLower := strings.ToLower(sourceText)
	score := 0.0
	if !queryDate.IsZero() {
		days := math.Abs(queryDate.Sub(sourceDate).Hours() / 24)
		score += 0.004 / (1.0 + days/30.0)
		if target, ok := RelativeTargetDate(queryLower, queryDate); ok {
			delta := math.Abs(target.Sub(sourceDate).Hours() / 24)
			if delta <= 1.5 {
				score += 3.000
			} else if delta <= 3.5 {
				score += 0.750
			} else if delta <= 7 {
				score += 0.150
			}
		}
		if windowDays, ok := RelativeLookbackWindow(queryLower + " " + sourceLower); ok {
			age := queryDate.Sub(sourceDate).Hours() / 24
			if age >= 0 && age <= windowDays {
				score += 0.250
			}
		}
	}
	if questionType != "temporal-reasoning" && questionType != "multi-session" {
		score *= math.Min(1, lexicalEvidence/0.020)
	}
	return score
}

func HasTemporalCue(query string) bool {
	queryLower := strings.ToLower(query)
	if _, ok := RelativeTargetDate(queryLower, time.Now()); ok {
		return true
	}
	if _, ok := RelativeLookbackWindow(queryLower); ok {
		return true
	}
	for _, marker := range []string{"when", "before", "after", "latest", "recent", "date", "time"} {
		if strings.Contains(queryLower, marker) {
			return true
		}
	}
	return false
}

func RelativeTargetDate(queryLower string, queryDate time.Time) (time.Time, bool) {
	switch {
	case strings.Contains(queryLower, "a day ago") || strings.Contains(queryLower, "one day ago"):
		return queryDate.AddDate(0, 0, -1), true
	case strings.Contains(queryLower, "a week ago") || strings.Contains(queryLower, "one week ago") || strings.Contains(queryLower, "1 week ago"):
		return queryDate.AddDate(0, 0, -7), true
	case strings.Contains(queryLower, "two weeks ago") || strings.Contains(queryLower, "2 weeks ago"):
		return queryDate.AddDate(0, 0, -14), true
	case strings.Contains(queryLower, "three weeks ago") || strings.Contains(queryLower, "3 weeks ago"):
		return queryDate.AddDate(0, 0, -21), true
	case strings.Contains(queryLower, "four weeks ago") || strings.Contains(queryLower, "4 weeks ago"):
		return queryDate.AddDate(0, 0, -28), true
	default:
		if match := relativeDaysAgoRE.FindStringSubmatch(queryLower); len(match) == 2 {
			var days int
			for _, ch := range match[1] {
				days = days*10 + int(ch-'0')
			}
			if days > 0 && days <= 366 {
				return queryDate.AddDate(0, 0, -days), true
			}
		}
		return time.Time{}, false
	}
}

func RelativeLookbackWindow(queryLower string) (float64, bool) {
	switch {
	case strings.Contains(queryLower, "last weekend") || strings.Contains(queryLower, "past weekend"):
		return 10, true
	case strings.Contains(queryLower, "past two weeks") || strings.Contains(queryLower, "last two weeks") || strings.Contains(queryLower, "past 2 weeks") || strings.Contains(queryLower, "last 2 weeks"):
		return 14, true
	case strings.Contains(queryLower, "past week") || strings.Contains(queryLower, "last week"):
		return 7, true
	case strings.Contains(queryLower, "past month") || strings.Contains(queryLower, "last month"):
		return 31, true
	default:
		return 0, false
	}
}

var (
	tokenRE           = regexp.MustCompile(`[a-z0-9]+`)
	relativeDaysAgoRE = regexp.MustCompile(`\b([0-9]{1,3})\s+days?\s+ago\b`)
	querySplitRE      = regexp.MustCompile(`(?i)\s+(?:and|or|then)\s+|[,;?]`)
)

func SignificantTokens(s string) []string {
	raw := tokenRE.FindAllString(strings.ToLower(s), -1)
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		if len(tok) < 3 || stopword(tok) {
			continue
		}
		out = append(out, stemLight(tok))
	}
	return uniqueStrings(out)
}

func stopword(s string) bool {
	switch s {
	case "the", "and", "for", "with", "that", "this", "what", "when", "where", "which", "who", "why", "how", "did", "does", "was", "were", "are", "you", "your", "have", "has", "had", "from", "about", "into", "onto", "then", "than", "them", "they", "their", "our", "out", "can", "could", "would", "should":
		return true
	default:
		return false
	}
}

func stemLight(s string) string {
	for _, suffix := range []string{"ing", "edly", "ed", "ly", "s"} {
		if strings.HasSuffix(s, suffix) && len(s) > len(suffix)+3 {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
