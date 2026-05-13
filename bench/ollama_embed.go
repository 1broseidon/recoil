package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOllamaHost = "http://localhost:11434"

// ollamaEmbedBatch sends a batch to Ollama's native /api/embed endpoint.
// The local-model context means cost is always reported as 0; latency is
// what actually matters here, and it's set by the model + hardware.
func ollamaEmbedBatch(ctx context.Context, host, model string, inputs []string) ([][]float64, float64, error) {
	host = resolveOllamaHost(host)
	body, err := json.Marshal(map[string]any{
		"model": model,
		"input": inputs,
	})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("ollama embed %d: %s (run `ollama serve` and `ollama pull %s`)",
			resp.StatusCode, truncBodyOllama(data), model)
	}
	var parsed struct {
		Embeddings [][]float64 `json:"embeddings"`
		Error      string      `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, 0, fmt.Errorf("decode ollama embed: %w", err)
	}
	if parsed.Error != "" {
		return nil, 0, fmt.Errorf("ollama embed error: %s", parsed.Error)
	}
	if len(parsed.Embeddings) != len(inputs) {
		return nil, 0, fmt.Errorf("ollama returned %d embeddings, expected %d", len(parsed.Embeddings), len(inputs))
	}
	return parsed.Embeddings, 0, nil
}

func resolveOllamaHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	}
	if host == "" {
		host = defaultOllamaHost
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/")
}

func truncBodyOllama(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "..."
	}
	return string(b)
}
