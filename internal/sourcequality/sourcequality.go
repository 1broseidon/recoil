package sourcequality

import (
	"path"
	"strings"

	"github.com/1broseidon/recoil/internal/pathmatch"
)

const (
	ClassAgentInstructions = "agent_instructions"
	ClassContributing      = "contributing"
	ClassDevelopers        = "developers"
	ClassSecurity          = "security"
	ClassRootReadme        = "root_readme"
	ClassReadme            = "readme"
	ClassOperational       = "operational"
	ClassChangelog         = "changelog"
	ClassExample           = "example"
	ClassFixture           = "fixture"
	ClassGenerated         = "generated_or_public_static"
	ClassProductDocs       = "product_docs"
)

const (
	ModeSearch = "search"
	ModeWake   = "wake"
)

type Info struct {
	DocClass             string
	PathDepth            int
	IsRootDoc            bool
	IsHiddenOperational  bool
	IsOperationalDoc     bool
	IsNoiseProne         bool
	IsSecurityDisclosure bool
}

type ClassOverride struct {
	Pattern  string
	DocClass string
}

type Options struct {
	ClassOverrides  []ClassOverride
	SearchBoosts    map[string]float64
	SearchPenalties map[string]float64
	WakeBoosts      map[string]float64
	WakePenalties   map[string]float64
}

func Classify(sourcePath string) Info {
	return ClassifyWithOptions(sourcePath, Options{})
}

func ClassifyWithOptions(sourcePath string, opts Options) Info {
	p := normalizePath(sourcePath)
	base := path.Base(p)
	depth := 0
	if p != "" {
		depth = strings.Count(p, "/")
	}
	info := Info{PathDepth: depth}
	info.IsRootDoc = depth == 0 && isRootOperationalBase(base)

	switch {
	case base == "agents.md" || base == "claude.md":
		info.DocClass = ClassAgentInstructions
	case base == "security.md" || p == ".github/security.md" || strings.HasSuffix(p, "/.well-known/security.txt") || p == ".well-known/security.txt":
		info.DocClass = ClassSecurity
		info.IsSecurityDisclosure = true
	case strings.HasPrefix(base, "contributing") || strings.HasPrefix(base, "contribute"):
		info.DocClass = ClassContributing
	case strings.HasPrefix(base, "developers") || strings.Contains(p, "developer-guide"):
		info.DocClass = ClassDevelopers
	case strings.Contains(p, "changelog") || strings.Contains(base, "release-notes"):
		info.DocClass = ClassChangelog
	case hasPathPart(p, "examples") || hasPathPart(p, "example"):
		info.DocClass = ClassExample
	case hasAnyPathPart(p, "fixtures", "fixture", "testdata", "evals"):
		info.DocClass = ClassFixture
	case hasAnyPathPart(p, "public", "static", "static-data", "i18n") || base == "robots.txt" || base == "humans.txt" || base == "lawyers.txt":
		info.DocClass = ClassGenerated
	case depth == 0 && strings.HasPrefix(base, "readme"):
		info.DocClass = ClassRootReadme
	case strings.HasPrefix(base, "readme"):
		info.DocClass = ClassReadme
	case hasAnyPathPart(p, "docs", "doc", "runtime/doc"):
		info.DocClass = ClassProductDocs
	}

	info.IsHiddenOperational = p == ".github/security.md" || strings.HasPrefix(p, ".github/") && info.DocClass == ClassSecurity
	info.IsOperationalDoc = info.IsRootDoc ||
		info.DocClass == ClassAgentInstructions ||
		info.DocClass == ClassContributing ||
		info.DocClass == ClassDevelopers ||
		info.DocClass == ClassSecurity
	info.IsNoiseProne = info.DocClass == ClassChangelog ||
		info.DocClass == ClassExample ||
		info.DocClass == ClassFixture ||
		info.DocClass == ClassGenerated
	if override, ok := matchingOverride(p, opts.ClassOverrides); ok {
		info.DocClass = override
		info.IsOperationalDoc = info.IsRootDoc ||
			info.DocClass == ClassAgentInstructions ||
			info.DocClass == ClassContributing ||
			info.DocClass == ClassDevelopers ||
			info.DocClass == ClassSecurity ||
			info.DocClass == ClassOperational
		info.IsNoiseProne = info.DocClass == ClassChangelog ||
			info.DocClass == ClassExample ||
			info.DocClass == ClassFixture ||
			info.DocClass == ClassGenerated
		info.IsSecurityDisclosure = info.DocClass == ClassSecurity
	}
	return info
}

func ScorePrior(query, sourcePath, metadataJSON string) float64 {
	return ScorePriorWithOptions(query, sourcePath, metadataJSON, ModeSearch, Options{})
}

func ScorePriorWithOptions(query, sourcePath, metadataJSON, mode string, opts Options) float64 {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourcePath)), "channel://") {
		return 0
	}
	info := ClassifyWithOptions(sourcePath, opts)
	q := strings.ToLower(query)
	score := 0.0

	if wantsSecurity(q) {
		if info.DocClass == ClassSecurity {
			score += 5.0
		} else if info.DocClass == ClassContributing {
			score += 0.75
		} else if info.IsNoiseProne {
			score -= 0.75
		}
	}

	if wantsOnboarding(q) {
		switch info.DocClass {
		case ClassAgentInstructions:
			score += 3.0
		case ClassContributing, ClassDevelopers:
			score += 2.25
		case ClassRootReadme:
			score += 1.25
		case ClassReadme:
			score += 0.35
		case ClassExample, ClassFixture, ClassGenerated, ClassChangelog:
			score -= 0.85
		}
	}

	// Explicit "contributor guide" / "contributing" intent — the canonical
	// answer is CONTRIBUTING.md regardless of how content-dense the README is.
	if wantsContributorGuide(q) {
		switch info.DocClass {
		case ClassContributing:
			score += 3.5
		case ClassDevelopers:
			score += 1.5
		case ClassRootReadme:
			score -= 0.5
		case ClassReadme:
			score -= 1.0
		}
	}

	if wantsTests(q) {
		switch info.DocClass {
		case ClassAgentInstructions, ClassContributing, ClassDevelopers:
			score += 1.25
		case ClassExample, ClassFixture:
			if !strings.Contains(q, "example") && !strings.Contains(q, "fixture") {
				score -= 0.65
			}
		}
	}

	if wantsRelease(q) {
		switch info.DocClass {
		case ClassContributing, ClassDevelopers, ClassRootReadme:
			score += 1.0
		case ClassChangelog:
			score -= 0.55
		case ClassSecurity:
			// release-process queries should NOT surface security-advisory or
			// vuln-disclosure docs even when they happen to mention release/publish.
			score -= 1.5
		}
	}

	if wantsOverview(q) {
		switch info.DocClass {
		case ClassRootReadme:
			score += 3.0
		case ClassReadme:
			score += 0.6
		case ClassAgentInstructions:
			score += 0.4
		case ClassContributing, ClassDevelopers:
			// These describe how to *work on* the project, not what the project is.
			score -= 0.4
		case ClassFixture, ClassGenerated, ClassExample, ClassChangelog:
			score -= 1.2
		}
	}

	// Non-English locale paths under docs/<lang>/... or site/<lang>/...
	// shouldn't beat the English equivalent for English queries. Apply a
	// general penalty whenever a translated doc surfaces and the query is in
	// English (we approximate "in English" by checking the query contains only
	// ASCII letters, which all our query battery queries do).
	if isEnglishQuery(q) && isLocalizedTranslation(sourcePath) {
		score -= 1.0
	}

	if isBroadQuery(q) {
		if info.IsOperationalDoc {
			score += 0.55
		}
		if info.IsNoiseProne {
			score -= 0.45
		}
		if info.PathDepth >= 4 && !pathIsExplicitlyMentioned(q, sourcePath) {
			score -= 0.15
		}
	}

	if strings.Contains(metadataJSON, `"doc_class":"`+info.DocClass+`"`) && info.IsOperationalDoc {
		score += 0.05
	}
	return score + configuredClassAdjustment(info.DocClass, mode, opts)
}

func OperationalHiddenPath(sourcePath string) bool {
	p := normalizePath(sourcePath)
	return p == ".github/security.md" ||
		p == ".well-known/security.txt" ||
		strings.HasSuffix(p, "/.well-known/security.txt")
}

func OperationalSourcePaths() []string {
	return []string{
		"AGENTS.md",
		"CLAUDE.md",
		"CONTRIBUTING.md",
		"CONTRIBUTING",
		"DEVELOPERS.md",
		"DEVELOPERS",
		"SECURITY.md",
		".github/SECURITY.md",
		".well-known/security.txt",
		"README.md",
	}
}

func normalizePath(sourcePath string) string {
	p := strings.ReplaceAll(strings.TrimSpace(sourcePath), "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return strings.ToLower(p)
}

func isRootOperationalBase(base string) bool {
	return base == "agents.md" ||
		base == "claude.md" ||
		base == "security.md" ||
		strings.HasPrefix(base, "contributing") ||
		strings.HasPrefix(base, "contribute") ||
		strings.HasPrefix(base, "developers") ||
		strings.HasPrefix(base, "developing") ||
		strings.HasPrefix(base, "readme")
}

func hasPathPart(p, part string) bool {
	for _, current := range strings.Split(p, "/") {
		if current == part {
			return true
		}
	}
	return false
}

func hasAnyPathPart(p string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(part, "/") {
			if strings.Contains(p, part) {
				return true
			}
			continue
		}
		if hasPathPart(p, part) {
			return true
		}
	}
	return false
}

func wantsSecurity(q string) bool {
	markers := []string{
		"security", "vulnerability", "vulnerable", "vuln",
		"disclosure", "hackerone", "exploit", "cve",
		"report a bug", "found exploit", "found a vulnerability", "responsible disclosure",
	}
	for _, m := range markers {
		if strings.Contains(q, m) {
			return true
		}
	}
	return false
}

// isEnglishQuery returns true for queries that look like English (ASCII-only
// letters). This is a conservative proxy — non-Latin scripts won't match so
// we keep our hands off mixed-language corpora.
func isEnglishQuery(q string) bool {
	for _, r := range q {
		if r > 127 {
			return false
		}
	}
	return true
}

// isLocalizedTranslation detects path patterns like docs/fr/foo.md,
// site/zh-CN/bar.md, doc/ja/baz.md — locale-prefixed translated content. The
// English locale ("en") is the canonical, not a translation.
func isLocalizedTranslation(sourcePath string) bool {
	p := normalizePath(sourcePath)
	if p == "" {
		return false
	}
	parts := strings.Split(p, "/")
	// We need at least docs/<lang>/<file>.
	if len(parts) < 3 {
		return false
	}
	for i := 0; i < len(parts)-1; i++ {
		switch parts[i] {
		case "docs", "doc", "documentation", "site", "guides", "source", "translations", "i18n":
			next := parts[i+1]
			if isLocaleCode(next) && next != "en" && next != "en-us" && next != "en-gb" {
				return true
			}
		}
	}
	return false
}

func isLocaleCode(s string) bool {
	if len(s) < 2 || len(s) > 7 {
		return false
	}
	// Plain 2-letter or 3-letter ISO code, or BCP-47 like "pt-br", "zh-cn".
	known := map[string]bool{
		"ar": true, "az": true, "bg": true, "bn": true, "ca": true, "cs": true,
		"da": true, "de": true, "el": true, "es": true, "et": true, "fa": true,
		"fi": true, "fr": true, "he": true, "hi": true, "hr": true, "hu": true,
		"id": true, "it": true, "ja": true, "ka": true, "ko": true, "lt": true,
		"lv": true, "mk": true, "mn": true, "ms": true, "my": true, "nb": true,
		"nl": true, "no": true, "pl": true, "pt": true, "ro": true, "ru": true,
		"si": true, "sk": true, "sl": true, "sq": true, "sr": true, "sv": true,
		"sw": true, "ta": true, "te": true, "th": true, "tr": true, "uk": true,
		"ur": true, "vi": true, "yo": true, "zh": true,
		"pt-br": true, "pt-pt": true, "zh-cn": true, "zh-tw": true, "zh-hk": true,
		"es-mx": true, "es-es": true, "fr-fr": true, "en-us": true, "en-gb": true,
		"en": true,
	}
	return known[strings.ToLower(s)]
}

// wantsContributorGuide detects queries explicitly asking for the contributor
// guide / contributing docs. README.md should not beat CONTRIBUTING.md here,
// no matter how dense its content.
func wantsContributorGuide(q string) bool {
	markers := []string{
		"contributor guide", "contributors guide",
		"contributor guidelines", "contributing guide",
		"contributing guidelines", "contribution guide",
		"how to contribute", "how do i contribute",
		"contributor docs", "contributors documentation",
		"contributing docs", "contributing documentation",
	}
	for _, m := range markers {
		if strings.Contains(q, m) {
			return true
		}
	}
	return false
}

// wantsOverview detects queries asking for the project's high-level summary —
// the canonical answer is the root README, not a content-dense CONTRIBUTING.
func wantsOverview(q string) bool {
	markers := []string{
		"what is this project", "what does this project",
		"what is this repo", "what does this repo",
		"about this project", "about the project",
		"about this repo", "about the repo",
		"project overview", "repo overview", "high level overview",
		"summary of the project", "what is this thing",
		"what does it do", "what does this do",
		"project description", "describe this project",
	}
	for _, m := range markers {
		if strings.Contains(q, m) {
			return true
		}
	}
	return false
}

func wantsOnboarding(q string) bool {
	return strings.Contains(q, "onboard") ||
		strings.Contains(q, "agent") ||
		strings.Contains(q, "before editing") ||
		strings.Contains(q, "set up") ||
		strings.Contains(q, "setup") ||
		strings.Contains(q, "development environment") ||
		strings.Contains(q, "contribute") ||
		strings.Contains(q, "contributor") ||
		strings.Contains(q, "work in this repo")
}

func wantsTests(q string) bool {
	return strings.Contains(q, "test") || strings.Contains(q, "tests")
}

func wantsRelease(q string) bool {
	return strings.Contains(q, "release") || strings.Contains(q, "publish")
}

func isBroadQuery(q string) bool {
	tokens := strings.Fields(q)
	return len(tokens) <= 7 || wantsOnboarding(q) || wantsTests(q) || wantsRelease(q)
}

func pathIsExplicitlyMentioned(q, sourcePath string) bool {
	p := normalizePath(sourcePath)
	return strings.Contains(q, p) || strings.Contains(q, path.Base(p))
}

func matchingOverride(sourcePath string, overrides []ClassOverride) (string, bool) {
	for _, override := range overrides {
		pattern := normalizePath(override.Pattern)
		docClass := strings.TrimSpace(override.DocClass)
		if pattern == "" || docClass == "" {
			continue
		}
		if pathmatch.Match(pattern, sourcePath) {
			return docClass, true
		}
	}
	return "", false
}

func configuredClassAdjustment(docClass, mode string, opts Options) float64 {
	if strings.TrimSpace(docClass) == "" {
		return 0
	}
	switch mode {
	case ModeWake:
		return opts.WakeBoosts[docClass] - opts.WakePenalties[docClass]
	default:
		return opts.SearchBoosts[docClass] - opts.SearchPenalties[docClass]
	}
}
