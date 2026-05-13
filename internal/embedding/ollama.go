package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	OllamaProvider     = "ollama"
	DefaultOllamaModel = "nomic-embed-text"
	DefaultOllamaHost  = "http://localhost:11434"

	ollamaHostEnv      = "OLLAMA_HOST"
	ollamaEmbedTimeout = 30 * time.Second
)

// OllamaEmbeddingProvider runs embeddings against a locally hosted Ollama
// server. No API key required; the only configuration is the base URL
// (default http://localhost:11434, overridable via OLLAMA_HOST) and the
// model name (default nomic-embed-text — 137M params, 768-d, widely
// available, decent retrieval quality).
//
// This intentionally talks to Ollama's native /api/embed endpoint rather
// than its OpenAI-compatible /v1/embeddings endpoint because the native
// shape is simpler and supports batching directly.
type OllamaEmbeddingProvider struct {
	host  string
	model string
	httpc *http.Client
}

// NewOllamaProvider builds the provider, reading OLLAMA_HOST from the env
// when set. We don't probe the server here — defer that to first Embed call
// so the provider is cheap to construct (matches LocalProvider's behavior).
func NewOllamaProvider(model string) (OllamaEmbeddingProvider, error) {
	host := strings.TrimSpace(os.Getenv(ollamaHostEnv))
	if host == "" {
		host = DefaultOllamaHost
	}
	// Ollama accepts hosts as host:port (no scheme); normalize to http:// for
	// the HTTP client. The OLLAMA_HOST convention is fuzzy in the wild.
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	host = strings.TrimRight(host, "/")
	if strings.TrimSpace(model) == "" {
		model = DefaultOllamaModel
	}
	return OllamaEmbeddingProvider{
		host:  host,
		model: model,
		httpc: &http.Client{Timeout: ollamaEmbedTimeout},
	}, nil
}

func (p OllamaEmbeddingProvider) Name() string  { return OllamaProvider }
func (p OllamaEmbeddingProvider) Model() string { return p.model }

func (p OllamaEmbeddingProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model": p.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	url := p.host + "/api/embed"
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
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
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("ollama embed %d: %s", resp.StatusCode, truncBody(body))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			// Surface the model-not-found error cleanly so operators know to
			// `ollama pull <model>` rather than chase a generic HTTP failure.
			return nil, fmt.Errorf("ollama embed %d: %s (check that `ollama serve` is running and `ollama pull %s` has succeeded)",
				resp.StatusCode, truncBody(body), p.model)
		}
		var parsed struct {
			Embeddings [][]float64 `json:"embeddings"`
			Error      string      `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decode ollama embed response: %w (body=%s)", err, truncBody(body))
		}
		if parsed.Error != "" {
			return nil, fmt.Errorf("ollama embed error: %s", parsed.Error)
		}
		if len(parsed.Embeddings) == 0 {
			return nil, fmt.Errorf("ollama returned no embeddings (body=%s)", truncBody(body))
		}
		return parsed.Embeddings[0], nil
	}
	return nil, fmt.Errorf("ollama embed exhausted retries: %w", lastErr)
}
