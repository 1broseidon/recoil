package main

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
	"time"
)

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	// Reasoning is the OpenRouter unified field for reasoning-model controls
	// (gpt-5.x, o-series, Claude with extended thinking). Omitted when nil so
	// non-reasoning models aren't sent an unrecognized parameter.
	Reasoning *reasoningParam `json:"reasoning,omitempty"`
}

type reasoningParam struct {
	Effort string `json:"effort,omitempty"` // "minimal" | "low" | "medium" | "high"
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type ChatResult struct {
	Content      string
	PromptTok    int
	CompletionTok int
	CostUSD      float64
}

type OpenRouterClient struct {
	apiKey  string
	httpc   *http.Client
	referer string
	title   string
}

func NewOpenRouterClient() (*OpenRouterClient, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, errors.New("OPENROUTER_API_KEY not set")
	}
	return &OpenRouterClient{
		apiKey:  key,
		httpc:   &http.Client{Timeout: 120 * time.Second},
		referer: "https://github.com/1broseidon/recoil",
		title:   "recoil-bench",
	}, nil
}

// CompleteOptions is optional configuration for a single completion call. The
// Reasoning field maps to OpenRouter's unified reasoning controls; non-reasoning
// models ignore it.
type CompleteOptions struct {
	ReasoningEffort string // "minimal" | "low" | "medium" | "high" — empty = provider default
}

func (c *OpenRouterClient) Complete(ctx context.Context, model string, messages []ChatMessage, temperature float64, maxTokens int) (ChatResult, error) {
	return c.CompleteWithOptions(ctx, model, messages, temperature, maxTokens, CompleteOptions{})
}

func (c *OpenRouterClient) CompleteWithOptions(ctx context.Context, model string, messages []ChatMessage, temperature float64, maxTokens int, opts CompleteOptions) (ChatResult, error) {
	req := chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	if opts.ReasoningEffort != "" {
		req.Reasoning = &reasoningParam{Effort: opts.ReasoningEffort}
	}
	reqBody, err := json.Marshal(req)
	if err != nil {
		return ChatResult{}, err
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return ChatResult{}, ctx.Err()
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(reqBody))
		if err != nil {
			return ChatResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("HTTP-Referer", c.referer)
		req.Header.Set("X-Title", c.title)
		resp, err := c.httpc.Do(req)
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
			lastErr = fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncBody(body))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return ChatResult{}, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncBody(body))
		}
		var parsed chatResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return ChatResult{}, fmt.Errorf("decode response: %w (body=%s)", err, truncBody(body))
		}
		if parsed.Error != nil {
			return ChatResult{}, fmt.Errorf("openrouter error: %s", parsed.Error.Message)
		}
		if len(parsed.Choices) == 0 {
			return ChatResult{}, fmt.Errorf("openrouter returned no choices (body=%s)", truncBody(body))
		}
		return ChatResult{
			Content:       parsed.Choices[0].Message.Content,
			PromptTok:     parsed.Usage.PromptTokens,
			CompletionTok: parsed.Usage.CompletionTokens,
			CostUSD:       parsed.Usage.Cost,
		}, nil
	}
	return ChatResult{}, fmt.Errorf("openrouter exhausted retries: %w", lastErr)
}

func truncBody(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "..."
	}
	return string(b)
}
