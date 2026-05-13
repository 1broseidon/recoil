package pathmatch

import (
	"path"
	"strings"
)

func Match(pattern, rel string) bool {
	pattern = normalize(pattern)
	rel = normalize(rel)
	if pattern == "" || rel == "" {
		return false
	}
	if matched, _ := path.Match(pattern, rel); matched {
		return true
	}
	if !strings.Contains(pattern, "**") {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func normalize(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "./")
	return strings.Trim(value, "/")
}

func matchSegments(patterns, parts []string) bool {
	if len(patterns) == 0 {
		return len(parts) == 0
	}
	if patterns[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if matchSegments(patterns[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	matched, err := path.Match(patterns[0], parts[0])
	if err != nil || !matched {
		return false
	}
	return matchSegments(patterns[1:], parts[1:])
}
