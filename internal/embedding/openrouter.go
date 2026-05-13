package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	OpenRouterProvider    = "openrouter"
	DefaultOpenRouterModel = "openai/text-embedding-3-small"

	openRouterEmbedURL    = "https://openrouter.ai/api/v1/embeddings"
	openRouterReferer     = "https://github.com/1broseidon/recoil"
	openRouterTitle       = "recoil"
	openRouterAPIKeyEnv   = "OPENROUTER_API_KEY"
	openRouterDefaultTOut = 60 * time.Second
)

// OpenRouterEmbeddingProvider talks to OpenRouter's OpenAI-compatible
// /v1/embeddings endpoint. The API key comes from OPENROUTER_API_KEY; the
// model is configurable (default: openai/text-embedding-3-small, the cheap
// 1536-d model that benchmarks well on retrieval).
type OpenRouterEmbeddingProvider struct {
	apiKey string
	model  string
	httpc  *http.Client
}

// NewOpenRouterProvider constructs the provider, reading the API key from the
// environment. Returns an error if OPENROUTER_API_KEY is unset — callers can
// then fall back to the local provider or surface the error to the operator.
func NewOpenRouterProvider(model string) (OpenRouterEmbeddingProvider, error) {
	key := strings.TrimSpace(os.Getenv(openRouterAPIKeyEnv))
	if key == "" {
		return OpenRouterEmbeddingProvider{}, errors.New("OPENROUTER_API_KEY not set")
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultOpenRouterModel
	}
	return OpenRouterEmbeddingProvider{
		apiKey: key,
		model:  model,
		httpc:  &http.Client{Timeout: openRouterDefaultTOut},
	}, nil
}

func (p OpenRouterEmbeddingProvider) Name() string  { return OpenRouterProvider }
func (p OpenRouterEmbeddingProvider) Model() string { return p.model }

// Embed sends a single string to OpenRouter and returns the vector. We use the
// single-input form (input: "string") because the Provider interface is
// one-text-at-a-time; the batch path lives in the bench harness, which talks
// to OpenRouter directly when it needs throughput.
func (p OpenRouterEmbeddingProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model": p.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEmbedURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("HTTP-Referer", openRouterReferer)
		req.Header.Set("X-Title", openRouterTitle)
		resp, err := p.httpc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("openrouter embeddings %d: %s", resp.StatusCode, truncBody(body))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("openrouter embeddings %d: %s", resp.StatusCode, truncBody(body))
		}
		var parsed struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode embeddings response: %w (body=%s)", err, truncBody(body))
		}
		if len(parsed.Data) == 0 {
			return nil, fmt.Errorf("openrouter returned no embeddings (body=%s)", truncBody(body))
		}
		return parsed.Data[0].Embedding, nil
	}
	return nil, fmt.Errorf("openrouter embeddings exhausted retries: %w", lastErr)
}

func truncBody(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "..."
	}
	return string(b)
}
