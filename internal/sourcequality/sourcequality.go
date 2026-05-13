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
	case strings.HasPrefix(base, "contributing"):
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
		}
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
		strings.HasPrefix(base, "developers") ||
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
	return strings.Contains(q, "security") ||
		strings.Contains(q, "vulnerability") ||
		strings.Contains(q, "disclosure") ||
		strings.Contains(q, "hackerone")
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
