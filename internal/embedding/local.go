package embedding

import (
	"context"
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

const (
	DefaultLocalProvider = "local"
	DefaultLocalModel    = "local-hash-v1"
	localDims            = 256
)

type Provider interface {
	Name() string
	Model() string
	Embed(ctx context.Context, text string) ([]float64, error)
}

type LocalProvider struct {
	model string
}

func NewLocalProvider(model string) LocalProvider {
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultLocalModel
	}
	return LocalProvider{model: model}
}

func (p LocalProvider) Name() string {
	return DefaultLocalProvider
}

func (p LocalProvider) Model() string {
	return p.model
}

func (p LocalProvider) Embed(_ context.Context, text string) ([]float64, error) {
	vector := make([]float64, localDims)
	tokens := expandedTokens(text)
	for _, token := range tokens {
		addTerm(vector, token, 1)
	}
	for i := 0; i+1 < len(tokens); i++ {
		addTerm(vector, tokens[i]+"_"+tokens[i+1], 0.7)
	}
	normalize(vector)
	return vector, nil
}

var tokenRE = regexp.MustCompile(`[a-z0-9]+`)

func expandedTokens(text string) []string {
	raw := tokenRE.FindAllString(strings.ToLower(text), -1)
	tokens := make([]string, 0, len(raw)*2)
	for _, token := range raw {
		if len(token) <= 1 {
			continue
		}
		base := normalizeToken(token)
		tokens = append(tokens, base)
		for _, alias := range aliases(base) {
			tokens = append(tokens, alias)
		}
	}
	return tokens
}

func normalizeToken(token string) string {
	switch {
	case strings.HasSuffix(token, "ing") && len(token) > 5:
		return strings.TrimSuffix(token, "ing")
	case strings.HasSuffix(token, "ed") && len(token) > 4:
		return strings.TrimSuffix(token, "ed")
	case strings.HasSuffix(token, "s") && len(token) > 4:
		return strings.TrimSuffix(token, "s")
	default:
		return token
	}
}

func aliases(token string) []string {
	switch token {
	case "background", "daemon", "service", "hosted", "server", "process":
		return []string{"daemon", "service", "background"}
	case "remote", "cloud", "sync", "synchronization", "central", "centralized":
		return []string{"cloud", "sync", "remote"}
	case "database", "db", "sqlite", "storage":
		return []string{"sqlite", "database", "storage"}
	case "driver", "library", "dependency", "package":
		return []string{"dependency", "library", "driver"}
	case "fulltext", "fts", "fts5", "index", "indexing", "search":
		return []string{"search", "index", "fts5"}
	case "protocol", "mcp", "adapter", "integration", "connector":
		return []string{"integration", "protocol", "mcp"}
	case "hook", "hooks", "reminder", "instruction", "instructions":
		return []string{"hook", "instruction", "agent"}
	case "brainfile", "task", "tasks", "board", "backlog":
		return []string{"brainfile", "task", "backlog"}
	case "semantic", "meaning", "paraphrase", "embedding", "embeddings", "vector":
		return []string{"semantic", "embedding", "vector"}
	case "truth", "source", "reliable", "canonical", "authoritative":
		return []string{"truth", "source", "canonical"}
	case "current", "active", "live":
		return []string{"current", "active"}
	case "historical", "stale", "superseded", "rejected", "old":
		return []string{"historical", "stale"}
	default:
		return nil
	}
}

func addTerm(vector []float64, term string, weight float64) {
	if term == "" {
		return
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(term))
	idx := int(h.Sum64() % uint64(len(vector)))
	vector[idx] += weight
}

func normalize(vector []float64) {
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= norm
	}
}
