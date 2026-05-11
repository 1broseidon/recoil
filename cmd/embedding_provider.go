package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/recoil/internal/embedding"
)

func newEmbeddingProvider(providerName, model string) (embedding.Provider, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		providerName = embedding.DefaultLocalProvider
	}
	switch providerName {
	case embedding.DefaultLocalProvider:
		return embedding.NewLocalProvider(model), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", providerName)
	}
}
